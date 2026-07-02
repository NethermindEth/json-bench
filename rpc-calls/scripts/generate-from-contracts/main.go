package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"math/big"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"gopkg.in/yaml.v3"
)

type Config struct {
	ArgRegistry struct {
		Addresses []string `yaml:"addresses"`
		Uints     []uint64 `yaml:"uints"`
	} `yaml:"arg_registry"`
	Contracts []Contract `yaml:"contracts"`
}

type Contract struct {
	Name       string `yaml:"name"`
	Slug       string `yaml:"slug,omitempty"`
	Address    string `yaml:"address"`
	AbiAddress string `yaml:"abi_address,omitempty"`
	Category   string `yaml:"category"`
	ChainID    int    `yaml:"chain_id,omitempty"`
	AutoExpand bool   `yaml:"auto_expand,omitempty"`
	Calls      []Call `yaml:"calls"`
}

type Call struct {
	Method string `yaml:"method"`
	Args   []any  `yaml:"args,omitempty"`
}

type outRecord struct {
	Method string `json:"method"`
	Params []any  `json:"params"`
}

func main() {
	configPath := flag.String("config", "rpc-calls/scripts/generate-from-contracts/contracts.yaml", "YAML config listing contracts and calls")
	outputDir := flag.String("output-dir", "rpc-calls/contracts/", "directory to write <slug>-mainnet.jsonl files into")
	abiCache := flag.String("abi-cache", "rpc-calls/sources/contract-abis", "directory containing <address>.json ABI files (populated by init.sh)")
	maxPerContract := flag.Int("max-per-contract", 0, "if > 0, cap emitted records per contract (applied after shuffle)")
	contractsCSV := flag.String("contracts", "", "optional comma-separated whitelist of contract slugs (defaults to all)")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(*outputDir, 0o755); err != nil {
		slog.Error("mkdir output-dir", "error", err)
		os.Exit(1)
	}

	whitelist := parseWhitelist(*contractsCSV)

	for i := range cfg.Contracts {
		c := &cfg.Contracts[i]
		applyDefaults(c)

		if len(whitelist) > 0 && !whitelist[c.Slug] {
			continue
		}

		if err := validateContract(c); err != nil {
			slog.Error("invalid contract", "contract", c.Name, "error", err)
			os.Exit(1)
		}

		records, err := buildRecords(c, *abiCache)
		if err != nil {
			slog.Error("build records", "contract", c.Name, "error", err)
			os.Exit(1)
		}

		rand.Shuffle(len(records), func(i, j int) { records[i], records[j] = records[j], records[i] })
		if *maxPerContract > 0 && len(records) > *maxPerContract {
			records = records[:*maxPerContract]
		}

		outPath := filepath.Join(*outputDir, fmt.Sprintf("%s-mainnet.jsonl", c.Slug))
		if err := writeJSONL(outPath, records); err != nil {
			slog.Error("write output", "contract", c.Name, "path", outPath, "error", err)
			os.Exit(1)
		}

		fmt.Printf("%s: %d calls\n", c.Slug, len(records))
	}
}

func loadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &cfg, nil
}

func parseWhitelist(csv string) map[string]bool {
	if csv == "" {
		return nil
	}
	out := map[string]bool{}
	for _, raw := range strings.Split(csv, ",") {
		s := strings.TrimSpace(raw)
		if s != "" {
			out[s] = true
		}
	}
	return out
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func deriveSlug(name string) string {
	s := strings.ToLower(name)
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

func applyDefaults(c *Contract) {
	if c.Slug == "" {
		c.Slug = deriveSlug(c.Name)
	}
	if c.ChainID == 0 {
		c.ChainID = 1
	}
	if c.AbiAddress == "" {
		c.AbiAddress = c.Address
	}
}

func validateContract(c *Contract) error {
	if c.ChainID != 1 {
		return fmt.Errorf("chain_id=%d not supported (v1 is mainnet only)", c.ChainID)
	}
	if c.AutoExpand {
		return fmt.Errorf("auto_expand: true is not implemented in v1")
	}
	if !common.IsHexAddress(c.Address) {
		return fmt.Errorf("address %q is not a valid hex address", c.Address)
	}
	if len(c.Calls) == 0 {
		return fmt.Errorf("no calls defined")
	}
	return nil
}

func buildRecords(c *Contract, abiCache string) ([]outRecord, error) {
	abiPath := filepath.Join(abiCache, strings.ToLower(c.Address)+".json")
	rawABI, err := os.ReadFile(abiPath)
	if err != nil {
		return nil, fmt.Errorf("read ABI %s: %w (run init.sh first)", abiPath, err)
	}
	parsedABI, err := abi.JSON(strings.NewReader(string(rawABI)))
	if err != nil {
		return nil, fmt.Errorf("parse ABI %s: %w", abiPath, err)
	}

	to := common.HexToAddress(c.Address).Hex()

	records := make([]outRecord, 0, len(c.Calls))
	for i, call := range c.Calls {
		ctx := fmt.Sprintf("%s.calls[%d] %s", c.Name, i, call.Method)
		method, ok := parsedABI.Methods[call.Method]
		if !ok {
			return nil, fmt.Errorf("%s: method not in ABI", ctx)
		}
		if len(call.Args) != len(method.Inputs) {
			return nil, fmt.Errorf("%s: arg count %d != ABI input count %d (%s)",
				ctx, len(call.Args), len(method.Inputs), method.Sig)
		}

		converted := make([]any, len(call.Args))
		for j, arg := range call.Args {
			v, err := convertArg(arg, method.Inputs[j].Type, fmt.Sprintf("%s arg %d (%s)", ctx, j, method.Inputs[j].Type.String()))
			if err != nil {
				return nil, err
			}
			converted[j] = v
		}

		data, err := parsedABI.Pack(method.Name, converted...)
		if err != nil {
			return nil, fmt.Errorf("%s: pack: %w", ctx, err)
		}

		records = append(records, outRecord{
			Method: "eth_call",
			Params: []any{
				map[string]string{
					"to":   to,
					"data": hexutil.Encode(data),
				},
				"latest",
			},
		})
	}
	return records, nil
}

func convertArg(yamlArg any, t abi.Type, ctx string) (any, error) {
	switch t.T {
	case abi.AddressTy:
		s, ok := yamlArg.(string)
		if !ok {
			return nil, fmt.Errorf("%s: expected address hex string, got %T", ctx, yamlArg)
		}
		if !common.IsHexAddress(s) {
			return nil, fmt.Errorf("%s: invalid address %q", ctx, s)
		}
		return common.HexToAddress(s), nil

	case abi.BoolTy:
		b, ok := yamlArg.(bool)
		if !ok {
			return nil, fmt.Errorf("%s: expected bool, got %T", ctx, yamlArg)
		}
		return b, nil

	case abi.StringTy:
		s, ok := yamlArg.(string)
		if !ok {
			return nil, fmt.Errorf("%s: expected string, got %T", ctx, yamlArg)
		}
		return s, nil

	case abi.IntTy, abi.UintTy:
		return convertInt(yamlArg, ctx)

	case abi.FixedBytesTy:
		s, ok := yamlArg.(string)
		if !ok {
			return nil, fmt.Errorf("%s: expected hex string, got %T", ctx, yamlArg)
		}
		raw, err := hexutil.Decode(s)
		if err != nil {
			return nil, fmt.Errorf("%s: decode hex: %w", ctx, err)
		}
		if len(raw) != t.Size {
			return nil, fmt.Errorf("%s: hex decoded to %d bytes, expected %d", ctx, len(raw), t.Size)
		}
		arrayVal := reflect.New(reflect.ArrayOf(t.Size, reflect.TypeOf(byte(0)))).Elem()
		reflect.Copy(arrayVal, reflect.ValueOf(raw))
		return arrayVal.Interface(), nil

	default:
		return nil, fmt.Errorf("%s: ABI type %s is not supported in v1 (only address/bool/string/intN/uintN/bytesN)", ctx, t.String())
	}
}

func convertInt(yamlArg any, ctx string) (*big.Int, error) {
	switch v := yamlArg.(type) {
	case int:
		return big.NewInt(int64(v)), nil
	case int64:
		return big.NewInt(v), nil
	case uint64:
		return new(big.Int).SetUint64(v), nil
	case string:
		n, ok := new(big.Int).SetString(strings.TrimPrefix(strings.TrimPrefix(v, "0x"), "0X"), parseBase(v))
		if !ok {
			return nil, fmt.Errorf("%s: invalid integer literal %q", ctx, v)
		}
		return n, nil
	case float64:
		return nil, fmt.Errorf("%s: integer arg parsed as float (%v); quote it as a string to preserve precision", ctx, v)
	default:
		return nil, fmt.Errorf("%s: expected integer or numeric string, got %T", ctx, yamlArg)
	}
}

func parseBase(s string) int {
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		return 16
	}
	return 10
}

func writeJSONL(path string, records []outRecord) (err error) {
	if len(records) == 0 {
		return nil
	}
	f, err := os.OpenFile(path, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close %s: %w", path, cerr)
		}
	}()
	enc := json.NewEncoder(f)
	for _, r := range records {
		if err := enc.Encode(r); err != nil {
			return fmt.Errorf("encode %s: %w", path, err)
		}
	}
	return nil
}
