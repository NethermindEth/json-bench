// Command generate-erigon-compare ports the erigontech/rpc-tests corpus into
// json-bench `compare` configs.
//
// json-bench's `compare` is *differential* (it diffs each client's response
// against the others, not against a golden file), so this importer keeps only
// the requests from the Erigon corpus and drops the Erigon-captured expected
// responses. The value it adds is coverage (100+ methods, thousands of real
// mainnet requests) and, crucially, every ported test is runnable on any
// synced node: the Erigon tests pin specific historical blocks (which only an
// archive node can answer), so each test's block reference is rewritten to a
// runnable target (default `latest`) — no archive dependency.
//
// Block handling:
//   - A method's block-number / block-tag argument is rewritten to the target
//     (via a per-method arg map; eth_getLogs' fromBlock/toBlock are set too).
//   - Requests addressed by an immutable HASH (block hash, tx hash) that only
//     FETCH data (getBlockByHash, getTransactionByHash, receipts, …) are kept
//     as-is — any full node retains that data.
//   - Requests that REPLAY a fixed point by hash (trace_transaction,
//     debug_traceTransaction, debug_traceBlockByHash, …) cannot be retargeted
//     to a live block and are DROPPED (reported in the manifest).
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var blockTags = map[string]bool{
	"latest": true, "pending": true, "safe": true, "finalized": true, "earliest": true,
}

// numRe matches a short hex block number (fits in a machine word); a 32-byte
// hash is longer and therefore left untouched.
var numRe = regexp.MustCompile(`^0x[0-9a-fA-F]{1,15}$`)

var divergentPrefixes = []string{
	"erigon_", "ots_", "parity_", "engine_", "admin_", "txpool_", "trace_",
}

var divergentMethods = map[string]bool{
	"web3_clientVersion": true, "eth_coinbase": true, "eth_mining": true, "eth_getWork": true,
	"eth_submitWork": true, "eth_submitHashrate": true, "eth_syncing": true, "eth_hashrate": true,
	"net_listening": true, "net_peerCount": true, "eth_getFilterChanges": true, "eth_getFilterLogs": true,
	"eth_sendRawTransaction": true, "eth_fillTransaction": true, "eth_gasPrice": true,
	"eth_maxPriorityFeePerGas": true, "unknown_method": true,
}

// dropHashReplay lists methods whose state is addressed only by a fixed hash:
// they cannot be retargeted to a live block and replaying the referenced point
// needs archive state, so they are dropped from the suite.
var dropHashReplay = map[string]bool{
	"trace_transaction": true, "trace_replayTransaction": true, "debug_traceTransaction": true,
	"debug_traceBlockByHash": true, "debug_getModifiedAccountsByHash": true,
	"ots_traceTransaction": true, "ots_getInternalOperations": true, "ots_getTransactionError": true,
}

// blockArgIndex gives the positional index of the block-number/tag argument
// that is rewritten to the target. eth_getLogs is handled specially
// (fromBlock/toBlock live in the filter object).
var blockArgIndex = map[string]int{
	"eth_call": 1, "eth_estimateGas": 1, "eth_createAccessList": 1,
	"eth_getBalance": 1, "eth_getCode": 1, "eth_getTransactionCount": 1,
	"eth_getStorageAt": 2, "eth_getProof": 2, "eth_getBlockByNumber": 0,
	"eth_getBlockReceipts": 0, "eth_getBlockTransactionCountByNumber": 0,
	"eth_getTransactionByBlockNumberAndIndex":    0,
	"eth_getRawTransactionByBlockNumberAndIndex": 0,
	"eth_getUncleByBlockNumberAndIndex":          0, "eth_getUncleCountByBlockNumber": 0,
	"eth_feeHistory": 1, "eth_simulateV1": 1,
	"debug_traceBlockByNumber": 0, "debug_traceCall": 1, "debug_accountAt": 0,
	"debug_accountRange": 0, "debug_storageRangeAt": 0,
	"debug_getModifiedAccountsByNumber": 0, "debug_getRawBlock": 0,
	"debug_getRawHeader": 0, "debug_getRawReceipts": 0,
	"trace_block": 0, "trace_call": 2, "trace_replayBlockTransactions": 0,
	"erigon_getHeaderByNumber": 0, "erigon_getBalanceChangesInBlock": 0,
	"ots_getBlockDetails": 0, "ots_getBlockTransactions": 0,
}

// callOut is a single {id, params} entry in a compare config. Params is a raw
// YAML node so string scalars (addresses, hashes, hex quantities) are emitted
// quoted and object key order is preserved from the upstream fixture.
type callOut struct {
	ID     string     `yaml:"id"`
	Params *yaml.Node `yaml:"params"`
}

// compareConfig is the on-disk shape the `runner compare` loader expects.
type compareConfig struct {
	Name        string               `yaml:"name"`
	Description string               `yaml:"description"`
	Calls       map[string][]callOut `yaml:"calls"`
}

type comparisonRule struct {
	Method string `yaml:"method,omitempty"`
	Path   string `yaml:"path,omitempty"`
	Kind   string `yaml:"kind"`
}

type rulesConfig struct {
	Comparison struct {
		Rules []comparisonRule `yaml:"rules"`
	} `yaml:"comparison"`
}

func isDivergent(method string) bool {
	if divergentMethods[method] {
		return true
	}
	for _, p := range divergentPrefixes {
		if strings.HasPrefix(method, p) {
			return true
		}
	}
	return false
}

// remapBlockNode rewrites any block-number/tag argument of a params sequence
// to target, in place.
func remapBlockNode(method string, params *yaml.Node, target string) {
	if method == "eth_getLogs" {
		if len(params.Content) > 0 && params.Content[0].Kind == yaml.MappingNode {
			filter := params.Content[0]
			// A blockHash filter is an immutable point — leave it.
			if mapValue(filter, "blockHash") == nil {
				setMapString(filter, "fromBlock", target)
				setMapString(filter, "toBlock", target)
			}
		}
		return
	}
	idx, ok := blockArgIndex[method]
	if !ok || idx >= len(params.Content) {
		return
	}
	arg := params.Content[idx]
	if arg.Kind == yaml.ScalarNode && arg.Tag == "!!str" && (blockTags[arg.Value] || numRe.MatchString(arg.Value)) {
		arg.Value = target
		arg.Style = yaml.SingleQuotedStyle
	}
}

func mapValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func setMapString(m *yaml.Node, key, val string) {
	if v := mapValue(m, key); v != nil {
		*v = yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: val, Style: yaml.SingleQuotedStyle}
		return
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: val, Style: yaml.SingleQuotedStyle})
}

type rawEntry struct {
	Request *struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	} `json:"request"`
}

type bucket struct {
	calls map[string][]callOut
	seen  map[string]bool
	count int
}

func newBucket() *bucket {
	return &bucket{calls: map[string][]callOut{}, seen: map[string]bool{}}
}

func main() {
	source := flag.String("source", "rpc-calls/sources/erigon-rpc-tests/integration/mainnet", "root directory containing per-method subdirectories of upstream test_*.json files")
	out := flag.String("out", "config/compare/erigon", "directory to write the compare configs, rules and manifest into")
	network := flag.String("network", "mainnet", "network name used in the output file names and descriptions")
	targetBlock := flag.String("target-block", "latest", "block tag/number every test is retargeted to")
	sourceRef := flag.String("source-ref", "", "upstream rpc-tests git ref, recorded in the descriptions and manifest for provenance")
	flag.Parse()

	if err := run(*source, *out, *network, *targetBlock, *sourceRef); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run(source, out, network, targetBlock, sourceRef string) error {
	info, err := os.Stat(source)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("corpus dir not found: %s", source)
	}

	core, divergent := newBucket(), newBucket()
	dropped := map[string]int{}

	methodDirs, err := os.ReadDir(source)
	if err != nil {
		return fmt.Errorf("read source dir %q: %w", source, err)
	}
	for _, md := range methodDirs {
		if !md.IsDir() {
			continue
		}
		method := md.Name()
		mdir := filepath.Join(source, method)
		files, err := os.ReadDir(mdir)
		if err != nil {
			return fmt.Errorf("read method dir %q: %w", mdir, err)
		}
		names := make([]string, 0, len(files))
		for _, f := range files {
			if !f.IsDir() && strings.HasPrefix(f.Name(), "test_") && strings.HasSuffix(f.Name(), ".json") {
				names = append(names, f.Name())
			}
		}
		sort.Strings(names)

		for _, fname := range names {
			entries, err := readEntries(filepath.Join(mdir, fname))
			if err != nil {
				// Malformed or unreadable fixtures are skipped, mirroring the
				// original importer's best-effort corpus walk.
				continue
			}
			stem := strings.TrimSuffix(fname, ".json")
			for i, e := range entries {
				if e.Request == nil {
					continue
				}
				rpc := e.Request.Method
				if rpc == "" {
					rpc = method
				}
				if dropHashReplay[rpc] {
					dropped[rpc]++
					continue
				}
				params, err := paramsNode(e.Request.Params)
				if err != nil {
					continue
				}
				remapBlockNode(rpc, params, targetBlock)

				b := core
				if isDivergent(rpc) {
					b = divergent
				}
				key := rpc + "\x00" + canonicalJSON(params)
				if b.seen[key] {
					continue
				}
				b.seen[key] = true

				id := stem
				if i != 0 {
					id = fmt.Sprintf("%s_%d", stem, i)
				}
				b.calls[rpc] = append(b.calls[rpc], callOut{ID: id, Params: params})
				b.count++
			}
		}
	}

	if err := os.MkdirAll(out, 0o755); err != nil {
		return fmt.Errorf("mkdir out: %w", err)
	}

	prov := ""
	if sourceRef != "" {
		prov = fmt.Sprintf(" (source rpc-tests %s)", sourceRef)
	}
	desc := map[string]string{
		"core":      "standard eth_/debug_ methods; compares cleanly across clients",
		"divergent": "non-standard namespaces / node-local / oracle methods; informational (noisy cross-client)",
	}
	buckets := map[string]*bucket{"core": core, "divergent": divergent}
	for _, name := range []string{"core", "divergent"} {
		b := buckets[name]
		cfgName := "erigon-" + network
		fileName := "erigon-" + network + ".yaml"
		if name != "core" {
			cfgName += "-" + name
			fileName = "erigon-" + network + "-" + name + ".yaml"
		}
		cfg := compareConfig{
			Name: cfgName,
			Description: fmt.Sprintf(
				"Ported from erigontech/rpc-tests%s. %s. Every test retargeted to block '%s' — runnable on any synced node. Differential; pair with erigon-%s-rules.yaml.",
				prov, desc[name], targetBlock, network),
			Calls: b.calls,
		}
		if err := writeYAML(filepath.Join(out, fileName), cfg); err != nil {
			return err
		}
	}

	rules := rulesConfig{}
	rules.Comparison.Rules = []comparisonRule{
		{Method: "eth_getBlockByNumber", Path: "totalDifficulty", Kind: "ignore"},
		{Method: "eth_getBlockByHash", Path: "totalDifficulty", Kind: "ignore"},
		{Kind: "error_code_only"},
	}
	if err := writeYAML(filepath.Join(out, fmt.Sprintf("erigon-%s-rules.yaml", network)), rules); err != nil {
		return err
	}

	if err := writeManifest(out, network, targetBlock, prov, core.count, divergent.count, dropped); err != nil {
		return err
	}

	fmt.Printf("Wrote %d runnable requests (target block '%s') to %s\n", core.count+divergent.count, targetBlock, out)
	fmt.Printf("  core       %d\n", core.count)
	fmt.Printf("  divergent  %d\n", divergent.count)
	if len(dropped) > 0 {
		total := 0
		names := make([]string, 0, len(dropped))
		for m, n := range dropped {
			total += n
			names = append(names, m)
		}
		sort.Strings(names)
		fmt.Printf("  dropped    %d (hash-addressed replay: %s)\n", total, strings.Join(names, ", "))
	}
	return nil
}

// readEntries loads a fixture file as a list of entries. A file holding a bare
// object (not an array) is treated as a single-element list.
func readEntries(path string) ([]rawEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var arr []rawEntry
	if err := json.Unmarshal(data, &arr); err == nil {
		return arr, nil
	}
	var single rawEntry
	if err := json.Unmarshal(data, &single); err != nil {
		return nil, err
	}
	return []rawEntry{single}, nil
}

// paramsNode decodes a raw JSON-RPC params value into a YAML sequence node,
// preserving object key order and quoting every string scalar. A missing or
// null params becomes an empty flow sequence (`[]`).
func paramsNode(raw json.RawMessage) (*yaml.Node, error) {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	node, err := decodeJSONValue(dec)
	if err != nil {
		return nil, err
	}
	if node.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("params is not a list")
	}
	return node, nil
}

func decodeJSONValue(dec *json.Decoder) (*yaml.Node, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '[':
			n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			for dec.More() {
				child, err := decodeJSONValue(dec)
				if err != nil {
					return nil, err
				}
				n.Content = append(n.Content, child)
			}
			if _, err := dec.Token(); err != nil { // consume ']'
				return nil, err
			}
			if len(n.Content) == 0 {
				n.Style = yaml.FlowStyle
			}
			return n, nil
		case '{':
			n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyTok.(string)
				if !ok {
					return nil, fmt.Errorf("object key is not a string")
				}
				val, err := decodeJSONValue(dec)
				if err != nil {
					return nil, err
				}
				keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
				// Hex-address / numeric map keys (stateOverrides, storage slots)
				// must be quoted or a YAML-1.1 reader parses them as integers.
				if !plainKeyOK(key) {
					keyNode.Style = yaml.SingleQuotedStyle
				}
				n.Content = append(n.Content, keyNode, val)
			}
			if _, err := dec.Token(); err != nil { // consume '}'
				return nil, err
			}
			if len(n.Content) == 0 {
				n.Style = yaml.FlowStyle
			}
			return n, nil
		default:
			return nil, fmt.Errorf("unexpected delimiter %q", t)
		}
	case string:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: t, Style: yaml.SingleQuotedStyle}, nil
	case json.Number:
		tag := "!!int"
		if strings.ContainsAny(t.String(), ".eE") {
			tag = "!!float"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: t.String()}, nil
	case bool:
		v := "false"
		if t {
			v = "true"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: v}, nil
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	default:
		return nil, fmt.Errorf("unexpected token %T", tok)
	}
}

// canonicalJSON renders a params node to JSON with object keys sorted, giving a
// stable key that collapses structurally-equal requests regardless of key
// order — the deduplication key.
func canonicalJSON(n *yaml.Node) string {
	var sb strings.Builder
	writeCanonical(&sb, n)
	return sb.String()
}

func writeCanonical(sb *strings.Builder, n *yaml.Node) {
	switch n.Kind {
	case yaml.SequenceNode:
		sb.WriteByte('[')
		for i, c := range n.Content {
			if i > 0 {
				sb.WriteByte(',')
			}
			writeCanonical(sb, c)
		}
		sb.WriteByte(']')
	case yaml.MappingNode:
		type kv struct {
			k string
			v *yaml.Node
		}
		pairs := make([]kv, 0, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			pairs = append(pairs, kv{n.Content[i].Value, n.Content[i+1]})
		}
		sort.Slice(pairs, func(i, j int) bool { return pairs[i].k < pairs[j].k })
		sb.WriteByte('{')
		for i, p := range pairs {
			if i > 0 {
				sb.WriteByte(',')
			}
			writeJSONString(sb, p.k)
			sb.WriteByte(':')
			writeCanonical(sb, p.v)
		}
		sb.WriteByte('}')
	default: // scalar
		switch n.Tag {
		case "!!str":
			writeJSONString(sb, n.Value)
		case "!!null":
			sb.WriteString("null")
		default: // int, float, bool
			sb.WriteString(n.Value)
		}
	}
}

func writeJSONString(sb *strings.Builder, s string) {
	b, _ := json.Marshal(s)
	sb.Write(b)
}

// plainKeyRe matches identifier-like keys that can never be misread as a
// number, hex, bool or null by a YAML reader and so are safe to emit unquoted.
var plainKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// yamlPlainScalars are the YAML-1.1 bool/null words that must be quoted even
// though they are identifier-shaped.
var yamlPlainScalars = map[string]bool{
	"true": true, "false": true, "yes": true, "no": true, "on": true, "off": true,
	"y": true, "n": true, "null": true,
}

func plainKeyOK(s string) bool {
	return plainKeyRe.MatchString(s) && !yamlPlainScalars[strings.ToLower(s)]
}

func writeYAML(path string, v interface{}) error {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func writeManifest(out, network, targetBlock, prov string, core, divergent int, dropped map[string]int) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# Erigon rpc-tests -> json-bench compare (%s)\n\n", network)
	fmt.Fprintf(&b, "Source: erigontech/rpc-tests%s\n\n", prov)
	fmt.Fprintf(&b, "Every test is retargeted to block `%s`, so the whole suite runs on any synced node (no archive dependency).\n\n", targetBlock)
	b.WriteString("| config | calls | notes |\n|---|---:|---|\n")
	fmt.Fprintf(&b, "| `erigon-%s.yaml` | %d | standard methods, compares cleanly across clients |\n", network, core)
	fmt.Fprintf(&b, "| `erigon-%s-divergent.yaml` | %d | informational (noisy cross-client) |\n\n", network, divergent)
	fmt.Fprintf(&b, "Total runnable: %d\n\n", core+divergent)

	if len(dropped) > 0 {
		b.WriteString("## Dropped (not adaptable to an arbitrary block)\n\n")
		b.WriteString("Replay/state addressed only by a fixed hash — no block argument to retarget, and replaying the referenced point needs archive state:\n\n")
		names := make([]string, 0, len(dropped))
		for m := range dropped {
			names = append(names, m)
		}
		sort.Strings(names)
		for _, m := range names {
			fmt.Fprintf(&b, "- `%s` (%d)\n", m, dropped[m])
		}
		b.WriteString("\n")
	}

	b.WriteString("## Run\n\n```bash\n")
	fmt.Fprintf(&b, "go run ./runner compare --config config/compare/erigon/erigon-%s.yaml \\\n", network)
	b.WriteString("  --clients config/clients/clients.yaml --client-refs nethermind,geth,reth \\\n")
	fmt.Fprintf(&b, "  --rules config/compare/erigon/erigon-%s-rules.yaml\n", network)
	b.WriteString("```\n\n")
	b.WriteString("For a moving-head node set `--target-block` to a fixed recent number when regenerating (default `latest` is deterministic across nodes parked at the same head).\n")

	return os.WriteFile(filepath.Join(out, fmt.Sprintf("MANIFEST-%s.md", network)), []byte(b.String()), 0o644)
}
