// Command generate-trace-historical mints the historical tracing corpus from a
// live node: the five request families of rpc-calls/trace-historical, over a
// block range the node can actually answer.
//
// The checked-in corpora pin block ranges deep in mainnet history, which only a
// node carrying that history can serve. A node brought up by syncing to the tip
// and keeping history from there on has no state below its floor and answers
// those fixtures with an error, so it needs a corpus minted against its own
// range. Point this at such a node and it discovers the floor itself.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const zeroAddress = "0x0000000000000000000000000000000000000000"

type client struct {
	url      string
	http     *http.Client
	attempts int
}

type rpcError struct {
	method  string
	code    int
	message string
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("%s: rpc error %d: %s", e.method, e.code, e.message)
}

func (c *client) call(method string, params ...any) (json.RawMessage, error) {
	if params == nil {
		params = []any{}
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		return nil, err
	}

	var last error
	for attempt := 0; attempt < c.attempts; attempt++ {
		result, err := c.once(method, body)
		if err == nil {
			return result, nil
		}
		// Only transport faults are retried: an rpc error is the node's answer,
		// and asking again returns the same one.
		var responded *rpcError
		if errors.As(err, &responded) {
			return nil, err
		}
		last = err
		time.Sleep(time.Duration(attempt+1) * 250 * time.Millisecond)
	}
	return nil, last
}

func (c *client) once(method string, body []byte) (json.RawMessage, error) {
	resp, err := c.http.Post(c.url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()

	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("%s: decode: %w", method, err)
	}
	if envelope.Error != nil {
		return nil, &rpcError{method: method, code: envelope.Error.Code, message: envelope.Error.Message}
	}
	return envelope.Result, nil
}

type block struct {
	number       uint64
	hash         string
	transactions []string
}

type outRecord struct {
	Method string `json:"method"`
	Params []any  `json:"params"`
}

type config struct {
	outputDir   string
	blocks      int
	minTx       int
	headLag     uint64
	from        uint64
	to          uint64
	seed        uint64
	writeConfig string
	clientName  string
	rates       string
	vus         int
	duration    string
	metricsURL  string
}

func main() {
	rpcURL := flag.String("rpc", "http://127.0.0.1:8545", "JSON-RPC endpoint to mint the corpus from")
	metricsURL := flag.String("metrics", "", "Prometheus metrics endpoint of the same node; when it reports a transaction index, the corpus is minted inside that index's covered range")
	outputDir := flag.String("output-dir", "rpc-calls/trace-historical", "parent directory; the corpus lands in blocks-<lowest>-<highest> under it")
	blocks := flag.Int("blocks", 20, "blocks to sample, one request per block per family")
	minTx := flag.Int("min-tx", 50, "skip blocks with fewer transactions than this")
	headLag := flag.Uint64("head-lag", 256, "stay this many blocks behind head so the fixtures survive reorgs")
	from := flag.Uint64("from", 0, "lowest block to sample; 0 discovers the node's floor")
	to := flag.Uint64("to", 0, "highest block to sample; 0 uses head minus head-lag")
	seed := flag.Uint64("seed", 1, "PRNG seed, so the same node and range mint the same corpus")
	writeConfig := flag.String("write-config", "", "also write a benchmark config pointing at the corpus, ready for `runner benchmark`")
	clientName := flag.String("client", "nethermind", "client name the written config benchmarks; must match an entry in your clients.yaml")
	rates := flag.String("rps", "1", "offered rate per call for the written config; a comma-separated list writes one config per rate, for a throughput sweep")
	vus := flag.Int("vus", 16, "concurrent virtual users the written config allows")
	duration := flag.String("duration", "600s", "how long the written config runs each rate")
	timeout := flag.Duration("timeout", 120*time.Second, "per-request timeout")
	attempts := flag.Int("attempts", 4, "attempts per request; only transport faults are retried")
	flag.Parse()

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.IdleConnTimeout = 5 * time.Second

	c := &client{url: *rpcURL, http: &http.Client{Timeout: *timeout, Transport: transport}, attempts: *attempts}
	cfg := config{
		outputDir: *outputDir, blocks: *blocks, minTx: *minTx, headLag: *headLag,
		from: *from, to: *to, seed: *seed, writeConfig: *writeConfig, clientName: *clientName,
		rates: *rates, vus: *vus, duration: *duration, metricsURL: *metricsURL,
	}

	if err := run(c, cfg); err != nil {
		slog.Error("generate", "error", err)
		os.Exit(1)
	}
}

func run(c *client, cfg config) error {
	head, err := hexUint(c, "eth_blockNumber")
	if err != nil {
		return err
	}
	if head <= cfg.headLag {
		return fmt.Errorf("head %d is not above head-lag %d", head, cfg.headLag)
	}

	top := cfg.to
	if top == 0 {
		top = head - cfg.headLag
	}
	bottom := cfg.from
	if bottom == 0 && cfg.metricsURL != "" {
		indexFrom, indexTo, err := indexCoverage(c.http, cfg.metricsURL)
		if err != nil {
			return err
		}
		if indexFrom > 0 {
			bottom = indexFrom
			if indexTo < top {
				top = indexTo
			}
			slog.Info("the node reports a transaction index; minting inside it", "from", indexFrom, "to", indexTo)
		}
	}
	if bottom == 0 {
		if bottom, err = discoverFloor(c, top); err != nil {
			return err
		}
		slog.Info("discovered the lowest block the node serves state for", "block", bottom)
	}
	if bottom >= top {
		return fmt.Errorf("the servable range is empty: %d is not below %d", bottom, top)
	}

	rng := rand.New(rand.NewPCG(cfg.seed, cfg.seed^0x9e3779b97f4a7c15))
	sampled, err := sample(c, rng, cfg, bottom, top)
	if err != nil {
		return err
	}
	slog.Info("sampled", "blocks", len(sampled), "lowest", sampled[0].number, "highest", sampled[len(sampled)-1].number)

	families := map[string][]outRecord{}
	for _, b := range sampled {
		// The last transaction of a block is the worst case for a node without a
		// per-transaction index: it has to replay every transaction ahead of it.
		last := b.transactions[len(b.transactions)-1]
		families["debug_traceTransaction-callTracer"] = append(families["debug_traceTransaction-callTracer"],
			outRecord{"debug_traceTransaction", []any{last, map[string]any{"tracer": "callTracer"}}})
		families["debug_traceTransaction-prestateTracer"] = append(families["debug_traceTransaction-prestateTracer"],
			outRecord{"debug_traceTransaction", []any{last, map[string]any{"tracer": "prestateTracer"}}})
		families["trace_transaction"] = append(families["trace_transaction"],
			outRecord{"trace_transaction", []any{last}})
		families["trace_replayTransaction-trace-stateDiff"] = append(families["trace_replayTransaction-trace-stateDiff"],
			outRecord{"trace_replayTransaction", []any{last, []string{"trace", "stateDiff"}}})
		families["debug_traceBlockByHash-callTracer"] = append(families["debug_traceBlockByHash-callTracer"],
			outRecord{"debug_traceBlockByHash", []any{b.hash, map[string]any{"tracer": "callTracer"}}})
	}

	dir := filepath.Join(cfg.outputDir, fmt.Sprintf("blocks-%d-%d", sampled[0].number, sampled[len(sampled)-1].number))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for family, records := range families {
		path := filepath.Join(dir, family+".jsonl")
		if err := writeJSONL(path, records); err != nil {
			return err
		}
		fmt.Printf("%-48s %d records\n", filepath.Base(path), len(records))
	}
	fmt.Printf("\ncorpus written to %s\n", dir)
	if cfg.writeConfig == "" {
		fmt.Printf("point the `file:` entries of a benchmark config at it, or pass --write-config\n")
		fmt.Printf("to have one written for you.\n")
		return nil
	}
	written, err := writeBenchmarkConfigs(cfg, dir, sampled[0].number, sampled[len(sampled)-1].number)
	if err != nil {
		return err
	}
	for _, path := range written {
		fmt.Printf("benchmark config written to %s\n", path)
	}
	fmt.Printf("run %s with\n", map[bool]string{true: "them in turn", false: "it"}[len(written) > 1])
	fmt.Printf("  go run ./runner benchmark --config %s --clients <your clients.yaml>\n", written[0])
	return nil
}

// writeBenchmarkConfigs renders one config per offered rate. A single rate keeps
// the path as given, so the common case stays one file; a sweep suffixes the rate,
// because the rate is the only thing that differs between the runs being compared.
func writeBenchmarkConfigs(cfg config, dir string, lowest, highest uint64) ([]string, error) {
	var written []string
	rates := strings.Split(cfg.rates, ",")
	for _, rate := range rates {
		rate = strings.TrimSpace(rate)
		parsed, err := strconv.Atoi(rate)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("--rps %q: every rate must be a positive whole number", cfg.rates)
		}

		path := cfg.writeConfig
		if len(rates) > 1 {
			extension := filepath.Ext(path)
			path = fmt.Sprintf("%s-rps%d%s", strings.TrimSuffix(path, extension), parsed, extension)
		}
		if err := writeBenchmarkConfig(cfg, dir, path, parsed, lowest, highest); err != nil {
			return nil, err
		}
		written = append(written, path)
	}
	return written, nil
}

// writeBenchmarkConfig renders the same five calls the checked-in
// trace-transaction-historical.yaml carries, against the corpus just written,
// so that minting and running are one step and no path is edited by hand.
func writeBenchmarkConfig(cfg config, dir, path string, rate int, lowest, highest uint64) error {
	var b strings.Builder
	fmt.Fprintf(&b, "test_name: \"Historical single-transaction tracing %d-%d\"\n", lowest, highest)
	fmt.Fprintf(&b, "description: \"Cold single-transaction traces over blocks %d-%d, minted from the node under test: "+
		"the last transaction of %d distinct blocks per endpoint, every block with at least %d transactions, sampled with seed %d. "+
		"debug_traceBlockByHash over the same blocks is the whole-block control. "+
		"Names carry no commas - k6 builds threshold sub-metric names from them.\"\n", lowest, highest, cfg.blocks, cfg.minTx, cfg.seed)
	fmt.Fprintf(&b, "clients:\n  - %s\n", cfg.clientName)
	fmt.Fprintf(&b, "duration: %q\nrps: %d\nvus: %d\ncalls:\n", cfg.duration, rate, cfg.vus)

	for _, call := range []struct{ name, file string }{
		{"debug_traceTransaction callTracer", "debug_traceTransaction-callTracer"},
		{"debug_traceTransaction prestateTracer", "debug_traceTransaction-prestateTracer"},
		{"trace_transaction", "trace_transaction"},
		{"trace_replayTransaction trace stateDiff", "trace_replayTransaction-trace-stateDiff"},
		{"debug_traceBlockByHash callTracer whole block", "debug_traceBlockByHash-callTracer"},
	} {
		fmt.Fprintf(&b, "  - name: %q\n", call.name)
		fmt.Fprintf(&b, "    file: \"./%s\"\n", filepath.ToSlash(filepath.Join(dir, call.file+".jsonl")))
		b.WriteString("    file_type: \"jsonl\"\n    weight: 1\n    thresholds: [\"p(99)<600000\"]\n")
	}

	if parent := filepath.Dir(path); parent != "." {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// indexCoverage reads the range the node's per-transaction index covers. Outside
// it a trace is answered by replaying the transactions ahead of the one asked
// for, which is a different code path at a different cost, so a corpus that
// strays outside measures a mixture of the two and compares nothing. Zero means
// the node publishes no such range, and the floor decides the range instead.
func indexCoverage(client *http.Client, url string) (uint64, uint64, error) {
	response, err := client.Get(url)
	if err != nil {
		return 0, 0, fmt.Errorf("metrics %s: %w", url, err)
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return 0, 0, fmt.Errorf("metrics %s: %w", url, err)
	}

	from := gauge(string(body), "nethermind_transaction_changeset_index_from")
	to := gauge(string(body), "nethermind_transaction_changeset_index_to")
	if from == 0 || to <= from {
		return 0, 0, nil
	}
	return from, to, nil
}

// gauge reads one unlabelled Prometheus gauge, which is how a node publishes a
// block height: one sample, no labels, a float that is a whole number.
func gauge(body, name string) uint64 {
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, name) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || (fields[0] != name && !strings.HasPrefix(fields[0], name+"{")) {
			continue
		}
		value, err := strconv.ParseFloat(fields[len(fields)-1], 64)
		if err != nil || value <= 0 {
			continue
		}
		return uint64(value)
	}
	return 0
}

// discoverFloor binary searches the lowest height whose state the node still
// answers for. A full archive answers at block 1 and the search ends after one
// probe; a node keeping a window of history answers only above its floor.
func discoverFloor(c *client, top uint64) (uint64, error) {
	servable := func(block uint64) (bool, error) {
		_, err := c.call("eth_getBalance", zeroAddress, blockParam(block))
		if err == nil {
			return true, nil
		}
		var responded *rpcError
		if errors.As(err, &responded) {
			return false, nil
		}
		return false, err
	}

	ok, err := servable(1)
	if err != nil {
		return 0, err
	}
	if ok {
		return 1, nil
	}
	if ok, err = servable(top); err != nil {
		return 0, err
	} else if !ok {
		return 0, fmt.Errorf("the node serves no state at block %d, so there is no range to sample", top)
	}

	low, high := uint64(1), top
	for low+1 < high {
		middle := low + (high-low)/2
		ok, err := servable(middle)
		if err != nil {
			return 0, err
		}
		if ok {
			high = middle
		} else {
			low = middle
		}
	}
	return high, nil
}

func sample(c *client, rng *rand.Rand, cfg config, bottom, top uint64) ([]block, error) {
	span := top - bottom
	seen := map[uint64]bool{}
	var out []block

	for attempts := 0; len(out) < cfg.blocks && attempts < cfg.blocks*20; attempts++ {
		number := bottom + uint64(rng.Float64()*float64(span))
		if seen[number] {
			continue
		}
		seen[number] = true

		b, err := fetchBlock(c, number)
		if err != nil {
			return nil, err
		}
		if len(b.transactions) < cfg.minTx {
			continue
		}
		out = append(out, b)
	}
	if len(out) < cfg.blocks {
		return nil, fmt.Errorf("only %d of %d blocks had at least %d transactions in %d-%d", len(out), cfg.blocks, cfg.minTx, bottom, top)
	}

	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].number < out[j-1].number; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, nil
}

func fetchBlock(c *client, number uint64) (block, error) {
	raw, err := c.call("eth_getBlockByNumber", blockParam(number), false)
	if err != nil {
		return block{}, err
	}
	var body struct {
		Hash         string   `json:"hash"`
		Transactions []string `json:"transactions"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return block{}, fmt.Errorf("block %d: %w", number, err)
	}
	return block{number: number, hash: body.Hash, transactions: body.Transactions}, nil
}

func hexUint(c *client, method string, params ...any) (uint64, error) {
	raw, err := c.call(method, params...)
	if err != nil {
		return 0, err
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0, fmt.Errorf("%s: %w", method, err)
	}
	value, err := strconv.ParseUint(text[2:], 16, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", method, err)
	}
	return value, nil
}

func blockParam(number uint64) string {
	return "0x" + strconv.FormatUint(number, 16)
}

func writeJSONL(path string, records []outRecord) (err error) {
	f, err := os.OpenFile(path, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	encoder := json.NewEncoder(f)
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
			return err
		}
	}
	return nil
}
