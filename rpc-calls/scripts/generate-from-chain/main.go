// generate-from-chain samples a live archive endpoint and mints the non-eth_call
// fixture families (blocks, receipts, transactions, logs, traces, state reads)
// for that chain. The upstream geth `cmd/workload` corpora that feed
// generate-from-history / -filter / -traces only exist for mainnet, so any other
// network has to derive its own inputs from the chain itself.
//
// Everything emitted is built from data the endpoint actually returned — block
// hashes, transaction hashes, log addresses and topics — so a fixture can only
// reference state that exists.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type outRecord struct {
	Method string `json:"method"`
	Params []any  `json:"params"`
}

type block struct {
	Number       string `json:"number"`
	Hash         string `json:"hash"`
	Transactions []struct {
		Hash string `json:"hash"`
		From string `json:"from"`
		To   string `json:"to"`
	} `json:"transactions"`
}

type logEntry struct {
	Address     string   `json:"address"`
	Topics      []string `json:"topics"`
	BlockNumber string   `json:"blockNumber"`
}

type client struct {
	url      string
	http     *http.Client
	attempts int
}

// rpcError marks a fault the endpoint reported about the request itself, as
// opposed to a transport fault. Only the latter is worth retrying.
type rpcError struct {
	method  string
	code    int
	message string
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("%s: rpc error %d: %s", e.method, e.code, e.message)
}

// call retries transport and decode faults. A sampling run makes tens of
// thousands of requests over minutes, long enough that a dropped connection
// somewhere in the path is expected rather than exceptional.
func (c *client) call(method string, params ...any) (json.RawMessage, error) {
	var lastErr error
	for attempt := 0; attempt < c.attempts; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
		res, err := c.callOnce(method, params)
		if err == nil {
			return res, nil
		}
		var rpcErr *rpcError
		if errors.As(err, &rpcErr) {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

func (c *client) callOnce(method string, params []any) (json.RawMessage, error) {
	if params == nil {
		params = []any{}
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Post(c.url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
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

type config struct {
	network        string
	outputDir      string
	proofOutputDir string
	samples        int
	oldestBlock    uint64
	headLag        uint64
	logWindow      uint64
	seed           uint64
	includeTrace   bool
	includeProof   bool
	proofSlots     int
}

// fileNameFor renders <family>-<network>.jsonl, preserving the corpus
// convention that a head-targeted variant carries its suffix *after* the
// network — eth_getProof-gnosis-latest.jsonl, matching the checked-in
// eth_getCode-mainnet-latest.jsonl and friends.
func fileNameFor(family, network string) string {
	if base, ok := strings.CutSuffix(family, "-latest"); ok {
		return fmt.Sprintf("%s-%s-latest.jsonl", base, network)
	}
	return fmt.Sprintf("%s-%s.jsonl", family, network)
}

// dirFor routes the eth_getProof families to their own directory so a corpus
// run over outputDir never picks them up. See emitProofs for why.
func (cfg config) dirFor(family string) string {
	if strings.HasPrefix(family, "eth_getProof") {
		return cfg.proofOutputDir
	}
	return cfg.outputDir
}

func main() {
	rpcURL := flag.String("rpc", "http://127.0.0.1:8545", "archive JSON-RPC endpoint to sample")
	network := flag.String("network", "gnosis", "filename suffix, e.g. gnosis")
	outputDir := flag.String("output-dir", "rpc-calls/", "directory to write <family>-<network>.jsonl into")
	proofOutputDir := flag.String("proof-output-dir", "rpc-calls/proofs/", "separate directory for the eth_getProof fixtures")
	samples := flag.Int("samples", 200, "number of blocks to sample")
	oldest := flag.Uint64("oldest-block", 1, "lowest block height to sample from")
	headLag := flag.Uint64("head-lag", 128, "stay this many blocks behind head so fixtures survive reorgs")
	logWindow := flag.Uint64("log-window", 1000, "eth_getLogs range width; must not exceed the node's per-request cap")
	seed := flag.Uint64("seed", 0, "PRNG seed; 0 picks one from the clock")
	includeTrace := flag.Bool("trace", true, "emit trace_* fixtures (requires the trace namespace)")
	includeProof := flag.Bool("proof", true, "emit eth_getProof fixtures into --proof-output-dir")
	proofSlots := flag.Int("proof-slots", 3, "how many populated storage slots to prove per account")
	timeout := flag.Duration("timeout", 120*time.Second, "per-request timeout")
	attempts := flag.Int("attempts", 4, "attempts per request; only transport faults are retried")
	flag.Parse()

	cfg := config{
		network: *network, outputDir: *outputDir, proofOutputDir: *proofOutputDir, samples: *samples,
		oldestBlock: *oldest, headLag: *headLag, logWindow: *logWindow,
		seed: *seed, includeTrace: *includeTrace, includeProof: *includeProof,
		proofSlots: *proofSlots,
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// See verify-calls: a reused half-closed keep-alive connection surfaces as a
	// truncated body on POST, which Go will not retry for us.
	transport.IdleConnTimeout = 5 * time.Second

	c := &client{
		url:      *rpcURL,
		http:     &http.Client{Timeout: *timeout, Transport: transport},
		attempts: *attempts,
	}

	if err := run(c, cfg); err != nil {
		slog.Error("generate", "error", err)
		os.Exit(1)
	}
}

func run(c *client, cfg config) error {
	if err := os.MkdirAll(cfg.outputDir, 0o755); err != nil {
		return err
	}
	if cfg.includeProof {
		if err := os.MkdirAll(cfg.proofOutputDir, 0o755); err != nil {
			return err
		}
	}

	head, err := hexUint(c, "eth_blockNumber")
	if err != nil {
		return err
	}
	if head <= cfg.headLag {
		return fmt.Errorf("head %d is below head-lag %d", head, cfg.headLag)
	}
	top := head - cfg.headLag
	if cfg.oldestBlock >= top {
		return fmt.Errorf("oldest-block %d is not below the sampling ceiling %d", cfg.oldestBlock, top)
	}

	seed := cfg.seed
	if seed == 0 {
		seed = uint64(time.Now().UnixNano())
	}
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	slog.Info("sampling", "head", head, "ceiling", top, "samples", cfg.samples, "seed", seed)

	blocks, err := sampleBlocks(c, rng, cfg, top)
	if err != nil {
		return err
	}
	if len(blocks) == 0 {
		return fmt.Errorf("no blocks sampled")
	}
	slog.Info("sampled blocks", "count", len(blocks))

	files := map[string][]outRecord{}
	emit := func(family string, r outRecord) {
		files[family] = append(files[family], r)
	}

	var txHashes []string
	for _, b := range blocks {
		hydrated := rng.IntN(2) == 1
		if rng.IntN(2) == 0 {
			emit("historical-blocks", outRecord{"eth_getBlockByNumber", []any{b.Number, hydrated}})
		} else {
			emit("historical-blocks", outRecord{"eth_getBlockByHash", []any{b.Hash, hydrated}})
		}
		emit("eth_getBlockReceipts", outRecord{"eth_getBlockReceipts", []any{b.Number}})
		emit("historical-blocks", outRecord{"eth_getBlockTransactionCountByNumber", []any{b.Number}})

		for _, tx := range b.Transactions {
			txHashes = append(txHashes, tx.Hash)
		}
	}

	rng.Shuffle(len(txHashes), func(i, j int) { txHashes[i], txHashes[j] = txHashes[j], txHashes[i] })
	if len(txHashes) > cfg.samples*2 {
		txHashes = txHashes[:cfg.samples*2]
	}
	for _, h := range txHashes {
		emit("transactions", outRecord{"eth_getTransactionByHash", []any{h}})
		emit("transactions", outRecord{"eth_getTransactionReceipt", []any{h}})
	}

	if err := emitLogs(c, rng, cfg, blocks, emit); err != nil {
		return err
	}
	contracts, err := emitState(c, cfg, blocks, emit)
	if err != nil {
		return err
	}
	if cfg.includeProof {
		if err := emitProofs(c, rng, cfg, contracts, emit); err != nil {
			return err
		}
	}
	if cfg.includeTrace {
		if err := emitTraces(c, blocks, txHashes, emit); err != nil {
			return err
		}
	}

	families := make([]string, 0, len(files))
	for f := range files {
		families = append(families, f)
	}
	for _, family := range families {
		records := files[family]
		rng.Shuffle(len(records), func(i, j int) { records[i], records[j] = records[j], records[i] })
		path := filepath.Join(cfg.dirFor(family), fileNameFor(family, cfg.network))
		if err := writeJSONL(path, records); err != nil {
			return err
		}
		fmt.Printf("%-40s %d records\n", filepath.Base(path), len(records))
	}
	return nil
}

// sampleBlocks draws heights between oldestBlock and top with a square-law bias
// towards older heights. A uniform draw over a 48M-block, 5-second chain would
// put nearly every sample in the recent, cheaply-indexed range and never touch
// the pre-merge era.
func sampleBlocks(c *client, rng *rand.Rand, cfg config, top uint64) ([]block, error) {
	span := top - cfg.oldestBlock
	seen := map[uint64]bool{}
	var out []block

	for attempts := 0; len(out) < cfg.samples && attempts < cfg.samples*4; attempts++ {
		u := rng.Float64()
		n := cfg.oldestBlock + uint64(u*u*float64(span))
		if seen[n] {
			continue
		}
		seen[n] = true

		raw, err := c.call("eth_getBlockByNumber", hexU64(n), true)
		if err != nil {
			return nil, err
		}
		var b block
		if err := json.Unmarshal(raw, &b); err != nil || b.Hash == "" {
			continue
		}
		out = append(out, b)
	}
	return out, nil
}

func emitLogs(c *client, rng *rand.Rand, cfg config, blocks []block, emit func(string, outRecord)) error {
	type seed struct {
		address string
		topic   string
		head    uint64
	}
	var seeds []seed
	unfilteredEmitted := 0

	rangeCap := probeRangeCap(c, blocks, cfg.logWindow)
	slog.Info("eth_getLogs range cap", "blocks", rangeCap)

	for _, b := range blocks {
		if len(seeds) >= cfg.samples {
			break
		}
		n := parseHexUint(b.Number)
		if n < cfg.logWindow {
			continue
		}
		raw, err := c.call("eth_getLogs", unfilteredWindow(n, n))
		if err != nil {
			return err
		}
		var entries []logEntry
		if err := json.Unmarshal(raw, &entries); err != nil {
			continue
		}
		for _, e := range entries {
			if e.Address == "" || len(e.Topics) == 0 {
				continue
			}
			seeds = append(seeds, seed{address: e.Address, topic: e.Topics[0], head: n})
			break
		}

		// An unfiltered scan is the heaviest shape a node will accept, but the
		// block-range and response-size caps are node config we cannot read.
		// These calls are expensive, so only a slice of the sample carries one.
		if unfilteredEmitted < cfg.samples/5 {
			if q, ok := widestAccepted(c, n, rangeCap, unfilteredWindow); ok {
				unfilteredEmitted++
				emit("get-logs", outRecord{"eth_getLogs", []any{q}})
			}
		}
	}

	rng.Shuffle(len(seeds), func(i, j int) { seeds[i], seeds[j] = seeds[j], seeds[i] })
	for _, s := range seeds {
		byAddress := func(from, to uint64) map[string]any {
			return map[string]any{
				"fromBlock": hexU64(from), "toBlock": hexU64(to), "address": s.address,
			}
		}
		byTopic := func(from, to uint64) map[string]any {
			return map[string]any{
				"fromBlock": hexU64(from), "toBlock": hexU64(to),
				"address": s.address, "topics": []any{s.topic},
			}
		}
		for _, build := range []func(uint64, uint64) map[string]any{byAddress, byTopic} {
			q, ok := widestAccepted(c, s.head, cfg.logWindow, build)
			if !ok {
				slog.Warn("no window the node would answer", "address", s.address, "head", s.head)
				continue
			}
			emit("get-logs", outRecord{"eth_getLogs", []any{q}})
		}
	}
	return nil
}

// widestAccepted returns the widest window ending at `head` that the node will
// answer, halving from `startWidth`. The response-size cap bites hardest on
// exactly the busiest addresses — the ones most worth having a fixture for — so
// narrowing beats dropping them.
func widestAccepted(c *client, head, startWidth uint64, build func(from, to uint64) map[string]any) (map[string]any, bool) {
	for width := startWidth; width >= 1; width /= 2 {
		if width > head {
			continue
		}
		q := build(head-width+1, head)
		if _, err := c.call("eth_getLogs", q); err == nil {
			return q, true
		}
	}
	return nil, false
}

// probeRangeCap finds the widest block range the node accepts at all. It probes
// against the oldest sampled block, where log density is lowest, so a rejection
// is the range limit rather than the response-size limit. Node config does not
// change between requests, so this runs once.
func probeRangeCap(c *client, blocks []block, ceiling uint64) uint64 {
	oldest := ^uint64(0)
	for _, b := range blocks {
		if n := parseHexUint(b.Number); n < oldest {
			oldest = n
		}
	}
	widest := uint64(1)
	for width := uint64(1); width <= ceiling; width *= 2 {
		if width > oldest {
			break
		}
		if _, err := c.call("eth_getLogs", unfilteredWindow(oldest-width+1, oldest)); err != nil {
			break
		}
		widest = width
	}
	return widest
}

func unfilteredWindow(from, to uint64) map[string]any {
	return map[string]any{"fromBlock": hexU64(from), "toBlock": hexU64(to)}
}

type stateTarget struct {
	address string
	block   string
}

func emitState(c *client, cfg config, blocks []block, emit func(string, outRecord)) ([]stateTarget, error) {
	var targets []stateTarget

	for _, b := range blocks {
		for _, tx := range b.Transactions {
			if tx.To == "" {
				continue
			}
			raw, err := c.call("eth_getCode", tx.To, b.Number)
			if err != nil {
				return nil, err
			}
			var code string
			if err := json.Unmarshal(raw, &code); err != nil || len(code) <= 2 {
				continue
			}
			targets = append(targets, stateTarget{address: tx.To, block: b.Number})
			break
		}
		if len(targets) >= cfg.samples {
			break
		}
	}

	for _, t := range targets {
		emit("eth_getCode", outRecord{"eth_getCode", []any{t.address, t.block}})
		emit("eth_getStorageAt", outRecord{"eth_getStorageAt", []any{t.address, "0x0", t.block}})
		emit("eth_getBalance", outRecord{"eth_getBalance", []any{t.address, t.block}})
		emit("eth_getTransactionCount", outRecord{"eth_getTransactionCount", []any{t.address, t.block}})
	}
	return targets, nil
}

// emitProofs writes eth_getProof fixtures into their own directory and their
// own files. They are kept apart from the rest of the corpus for two reasons:
// `runner compare` excludes eth_getProof from corpus mode outright (proofs need
// not be stored), and a flat-state archive answers it only at the head while a
// trie archive answers it at any height — so the two shapes are not
// interchangeable and must not be swept into one run.
//
// Output is split the same way the mainnet corpus splits it:
//
//	eth_getProof-<net>-latest.jsonl  — head-targeted, answerable by any node
//	eth_getProof-<net>.jsonl         — pinned heights, needs a trie archive
func emitProofs(c *client, rng *rand.Rand, cfg config, targets []stateTarget, emit func(string, outRecord)) error {
	if len(targets) == 0 {
		return nil
	}

	// Deduplicate by address, keeping the oldest height seen for each so the
	// historical set reaches as far back as the sample does.
	oldest := map[string]string{}
	for _, t := range targets {
		if prev, seen := oldest[t.address]; !seen || parseHexUint(t.block) < parseHexUint(prev) {
			oldest[t.address] = t.block
		}
	}
	addrs := make([]string, 0, len(oldest))
	for a := range oldest {
		addrs = append(addrs, a)
	}
	sort.Strings(addrs)
	rng.Shuffle(len(addrs), func(i, j int) { addrs[i], addrs[j] = addrs[j], addrs[i] })

	historical := supportsHistoricalProof(c, addrs, oldest)
	if historical {
		slog.Info("endpoint serves historical state proofs; emitting pinned proofs too")
	} else {
		slog.Warn("endpoint serves state proofs at head only; emitting the -latest set only")
	}

	for _, a := range addrs {
		keys := populatedSlots(c, a, cfg.proofSlots)

		// An account proof with no storage keys, and the same account with real
		// populated slots, walk different amounts of trie — keep both shapes.
		emit("eth_getProof-latest", outRecord{"eth_getProof", []any{a, []string{}, "latest"}})
		if len(keys) > 0 {
			emit("eth_getProof-latest", outRecord{"eth_getProof", []any{a, keys, "latest"}})
		}

		if historical {
			block := oldest[a]
			emit("eth_getProof", outRecord{"eth_getProof", []any{a, []string{}, block}})
			if len(keys) > 0 {
				emit("eth_getProof", outRecord{"eth_getProof", []any{a, keys, block}})
			}
		}
	}
	return nil
}

// supportsHistoricalProof distinguishes a trie archive from a flat-state one.
// Nethermind's flat state answers `State proofs at historical block N are not
// supported`; anything else that fails is treated as unsupported too, since a
// fixture we cannot confirm is not worth emitting. Several accounts are tried
// before concluding, so one account that happens to fail for its own reason
// cannot suppress the whole pinned set.
func supportsHistoricalProof(c *client, addrs []string, oldest map[string]string) bool {
	for i, a := range addrs {
		if i >= 3 {
			break
		}
		if _, err := c.call("eth_getProof", a, []string{}, oldest[a]); err == nil {
			return true
		}
	}
	return false
}

// populatedSlots returns storage keys that actually hold a value. Proving an
// empty slot yields an exclusion proof and barely touches the storage trie;
// proving a populated one walks it properly, which is the work worth measuring.
func populatedSlots(c *client, address string, limit int) []string {
	var keys []string
	for i := 0; i < 16 && len(keys) < limit; i++ {
		slot := hexU64(uint64(i))
		raw, err := c.call("eth_getStorageAt", address, slot, "latest")
		if err != nil {
			continue
		}
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			continue
		}
		if strings.Trim(strings.TrimPrefix(v, "0x"), "0") == "" {
			continue
		}
		keys = append(keys, slot)
	}
	return keys
}

func emitTraces(c *client, blocks []block, txHashes []string, emit func(string, outRecord)) error {
	probe := false
	for _, b := range blocks {
		if !probe {
			if _, err := c.call("trace_block", b.Number); err != nil {
				return fmt.Errorf("trace namespace unavailable (pass -trace=false): %w", err)
			}
			probe = true
		}
		emit("traces", outRecord{"trace_block", []any{b.Number}})
		emit("traces", outRecord{"trace_replayBlockTransactions", []any{b.Number, []string{"trace"}}})
	}
	for _, h := range txHashes {
		emit("traces", outRecord{"trace_transaction", []any{h}})
	}
	return nil
}

func writeJSONL(path string, records []outRecord) (err error) {
	if len(records) == 0 {
		return nil
	}
	f, err := os.OpenFile(path, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	enc := json.NewEncoder(f)
	for _, r := range records {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return nil
}

func hexUint(c *client, method string) (uint64, error) {
	raw, err := c.call(method)
	if err != nil {
		return 0, err
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, err
	}
	return parseHexUint(s), nil
}

func hexU64(n uint64) string { return fmt.Sprintf("0x%x", n) }

func parseHexUint(s string) uint64 {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	var n uint64
	for _, ch := range s {
		var d uint64
		switch {
		case ch >= '0' && ch <= '9':
			d = uint64(ch - '0')
		case ch >= 'a' && ch <= 'f':
			d = uint64(ch-'a') + 10
		case ch >= 'A' && ch <= 'F':
			d = uint64(ch-'A') + 10
		default:
			return n
		}
		n = n*16 + d
	}
	return n
}
