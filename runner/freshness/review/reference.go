package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/core/types"

	"github.com/jsonrpc-bench/runner/freshness/capture"
	"github.com/jsonrpc-bench/runner/freshness/ethrpc"
	"github.com/jsonrpc-bench/runner/freshness/probe"
	"github.com/jsonrpc-bench/runner/freshness/schema"
)

// Reference verdicts for a block identity.
const (
	RefVerified   = "verified"
	RefOrphaned   = "orphaned"
	RefUnverified = "unverified"
)

// VerificationReceiptsRoot means the reference receipts reproduce the
// header's receipts root, so expected logs and receipts are trustworthy.
const VerificationReceiptsRoot = "receipts_root"

// refRecord is the cached raw reference answer for one block hash. Review
// always evaluates from these records, so --offline replays exactly what an
// online run saw.
type refRecord struct {
	Hash           string          `json:"hash"`
	Number         uint64          `json:"number"`
	CanonicalHash  string          `json:"canonical_hash"`
	Header         json.RawMessage `json:"header"`
	Receipts       json.RawMessage `json:"receipts"`
	ReceiptsSource string          `json:"receipts_source,omitempty"`
	FetchError     string          `json:"fetch_error,omitempty"`
}

type chainRecord struct {
	URL           string `json:"rpc_url"`
	ClientVersion string `json:"client_version,omitempty"`
	ChainID       uint64 `json:"chain_id"`
	Head          uint64 `json:"head"`
	Finalized     uint64 `json:"finalized"`
	HasFinalized  bool   `json:"has_finalized"`
}

// Reference is the evaluated reference view of one block identity.
type Reference struct {
	Hash         string            `json:"hash"`
	Number       uint64            `json:"number"`
	Status       string            `json:"status"`
	Reason       string            `json:"reason,omitempty"`
	Verification string            `json:"verification,omitempty"`
	Finalized    bool              `json:"finalized"`
	TxCount      int               `json:"tx_count"`
	LogCount     int               `json:"log_count"`
	Expected     map[string]string `json:"-"`
}

type block struct {
	number uint64
	hash   string
}

// fetchReferences fills the cache for every block not cached yet (or all,
// when refresh is set).
func fetchReferences(ctx context.Context, cfg *Config, dir string, blocks []block, refresh bool) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	client := ethrpc.NewClient(ethrpc.Options{
		URL:     cfg.Reference.RPCURL,
		Headers: cfg.Reference.Headers,
		Timeout: time.Duration(cfg.Reference.RequestTimeoutMs) * time.Millisecond,
	}, schema.NewClock())

	chainPath := filepath.Join(dir, "_chain.json")
	if _, err := os.Stat(chainPath); refresh || err != nil {
		cr, err := fetchChain(ctx, client)
		if err != nil {
			return fmt.Errorf("reference node: %w", err)
		}
		cr.URL = probe.RedactURL(cfg.Reference.RPCURL)
		if err := capture.WriteJSON(chainPath, cr); err != nil {
			return err
		}
	}

	work := make(chan block)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error
	for i := 0; i < cfg.Reference.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for b := range work {
				path := filepath.Join(dir, b.hash+".json")
				if _, err := os.Stat(path); err == nil && !refresh {
					continue
				}
				rec := fetchBlock(ctx, client, b)
				if err := capture.WriteJSON(path, rec); err != nil {
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()
				}
			}
		}()
	}
	for _, b := range blocks {
		work <- b
	}
	close(work)
	wg.Wait()
	return errors.Join(errs...)
}

func fetchChain(ctx context.Context, c *ethrpc.Client) (*chainRecord, error) {
	cr := &chainRecord{}
	if raw, err := c.CallResult(ctx, "web3_clientVersion"); err == nil {
		_ = json.Unmarshal(raw, &cr.ClientVersion)
	}
	raw, err := c.CallResult(ctx, "eth_chainId")
	if err != nil {
		return nil, err
	}
	id, err := ethrpc.ParseBigQuantity(raw)
	if err != nil || !id.IsUint64() {
		return nil, fmt.Errorf("eth_chainId returned %s", raw)
	}
	cr.ChainID = id.Uint64()
	if raw, err = c.CallResult(ctx, "eth_blockNumber"); err != nil {
		return nil, err
	}
	if cr.Head, err = ethrpc.ParseQuantity(raw); err != nil {
		return nil, err
	}
	if raw, err := c.CallResult(ctx, "eth_getBlockByNumber", "finalized", false); err == nil {
		if h, err := ethrpc.ParseHeader(raw); err == nil && h != nil {
			cr.Finalized, cr.HasFinalized = h.Number, true
		}
	}
	return cr, nil
}

func fetchBlock(ctx context.Context, c *ethrpc.Client, b block) *refRecord {
	rec := &refRecord{Hash: b.hash, Number: b.number}
	fail := func(what string, err error) *refRecord {
		rec.FetchError = what + ": " + err.Error()
		return rec
	}
	raw, err := c.CallResult(ctx, "eth_getBlockByNumber", ethrpc.Quantity(b.number), false)
	if err != nil {
		return fail("eth_getBlockByNumber", err)
	}
	if h, err := ethrpc.ParseHeader(raw); err == nil && h != nil {
		rec.CanonicalHash = ethrpc.HashHex(h.Hash)
	}
	raw, err = c.CallResult(ctx, "eth_getBlockByHash", b.hash, false)
	if err != nil {
		return fail("eth_getBlockByHash", err)
	}
	rec.Header = raw
	h, err := ethrpc.ParseHeader(raw)
	if err != nil {
		return fail("header", err)
	}
	if h == nil {
		return rec
	}
	raw, err = c.CallResult(ctx, "eth_getBlockReceipts", b.hash)
	if err == nil && string(raw) != "null" {
		rec.Receipts, rec.ReceiptsSource = raw, "eth_getBlockReceipts"
		return rec
	}
	parts := make([]json.RawMessage, 0, len(h.TxHashes))
	for _, tx := range h.TxHashes {
		r, err := c.CallResult(ctx, "eth_getTransactionReceipt", ethrpc.HashHex(tx))
		if err != nil {
			return fail("eth_getTransactionReceipt", err)
		}
		parts = append(parts, r)
	}
	all, _ := json.Marshal(parts)
	rec.Receipts, rec.ReceiptsSource = all, "eth_getTransactionReceipt"
	return rec
}

// loadReferences evaluates the cache. A missing record is unverified: review
// never falls back to trusting a tested node.
func loadReferences(dir string, blocks []block) (map[string]*Reference, *chainRecord) {
	var chain *chainRecord
	var cr chainRecord
	if readJSON(filepath.Join(dir, "_chain.json"), &cr) == nil {
		chain = &cr
	}
	out := make(map[string]*Reference, len(blocks))
	for _, b := range blocks {
		var rec refRecord
		if err := readJSON(filepath.Join(dir, b.hash+".json"), &rec); err != nil {
			out[b.hash] = &Reference{Hash: b.hash, Number: b.number, Status: RefUnverified, Reason: "no reference data"}
			continue
		}
		out[b.hash] = evaluateReference(&rec, chain)
	}
	return out, chain
}

func evaluateReference(rec *refRecord, chain *chainRecord) *Reference {
	ref := &Reference{Hash: rec.Hash, Number: rec.Number, Expected: map[string]string{}}
	unverified := func(reason string) *Reference {
		ref.Status, ref.Reason = RefUnverified, reason
		return ref
	}
	if rec.FetchError != "" {
		return unverified("reference fetch failed: " + rec.FetchError)
	}
	h, err := ethrpc.ParseHeader(rec.Header)
	if err != nil {
		return unverified("reference header unparseable: " + err.Error())
	}
	if rec.CanonicalHash != "" && rec.CanonicalHash != rec.Hash {
		ref.Status, ref.Reason = RefOrphaned, "reference has a different canonical block at this height"
		return ref
	}
	if h == nil {
		ref.Status, ref.Reason = RefOrphaned, "reference does not know this block"
		return ref
	}
	if chain != nil && chain.HasFinalized {
		ref.Finalized = rec.Number <= chain.Finalized
	}
	receipts, err := ethrpc.CanonReceipts(rec.Receipts)
	if err != nil {
		return unverified("reference receipts unparseable: " + err.Error())
	}
	if len(receipts) != len(h.TxHashes) {
		return unverified(fmt.Sprintf("reference returned %d receipts for %d transactions", len(receipts), len(h.TxHashes)))
	}
	for _, r := range receipts {
		if r.BlockHash != rec.Hash {
			return unverified("reference receipt belongs to another block")
		}
	}
	if ethrpc.ReceiptsRoot(receipts) != h.ReceiptsRoot {
		return unverified("reference receipts do not reproduce the header receipts root")
	}
	logs := ethrpc.BlockLogs(receipts)
	if ethrpc.LogsBloom(logs.Logs) != h.LogsBloom && !(len(logs.Logs) == 0 && h.LogsBloom == (types.Bloom{})) {
		return unverified("reference logs do not reproduce the header bloom")
	}
	ref.Status, ref.Verification = RefVerified, VerificationReceiptsRoot
	ref.TxCount, ref.LogCount = len(receipts), len(logs.Logs)

	parent := ethrpc.Digest(ethrpc.HashHex(h.ParentHash))
	ref.Expected[schema.ProbeStateNumber] = parent
	ref.Expected[schema.ProbeStateLatest] = parent
	ref.Expected[schema.ProbeStateHashCanonical] = parent
	ref.Expected[schema.ProbeLogsNumber] = logs.Digest()
	ref.Expected[schema.ProbeLogsHash] = logs.Digest()
	ref.Expected[schema.ProbeBlockReceipts] = ethrpc.Digest(receipts)
	if len(receipts) > 0 {
		ref.Expected[schema.ProbeTransactionReceipt] = ethrpc.Digest(receipts[len(receipts)-1])
	}
	return ref
}

func sortedBlocks(set map[string]uint64) []block {
	out := make([]block, 0, len(set))
	for h, n := range set {
		out = append(out, block{number: n, hash: h})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].number != out[j].number {
			return out[i].number < out[j].number
		}
		return strings.Compare(out[i].hash, out[j].hash) < 0
	})
	return out
}
