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

// Refresh selects which cached reference records an online review fetches
// again. Records whose fetch failed are always fetched again.
type Refresh int

const (
	RefreshNone Refresh = iota
	RefreshUnfinalized
	RefreshAll
)

// fetchReferences fills the cache for every block that is missing, failed
// earlier, or selected by refresh.
func fetchReferences(ctx context.Context, cfg *Config, dir string, blocks []block, refresh Refresh) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	client := &refClient{
		c: ethrpc.NewClient(ethrpc.Options{
			URL:     cfg.Reference.RPCURL,
			Headers: cfg.Reference.Headers,
			Timeout: time.Duration(cfg.Reference.RequestTimeoutMs) * time.Millisecond,
		}, schema.NewClock()),
		attempts:  cfg.Reference.MaxAttempts,
		baseDelay: time.Duration(cfg.Reference.RetryBaseDelayMs) * time.Millisecond,
	}

	chainPath := filepath.Join(dir, "_chain.json")
	var chain chainRecord
	if err := readJSON(chainPath, &chain); refresh != RefreshNone || err != nil {
		cr, err := fetchChain(ctx, client)
		if err != nil {
			return fmt.Errorf("reference node: %w", err)
		}
		cr.URL = probe.RedactURL(cfg.Reference.RPCURL)
		if err := capture.WriteJSON(chainPath, cr); err != nil {
			return err
		}
		chain = *cr
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
				if !needsFetch(path, refresh, &chain) {
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

func needsFetch(path string, refresh Refresh, chain *chainRecord) bool {
	var rec refRecord
	if refresh == RefreshAll || readJSON(path, &rec) != nil || rec.FetchError != "" {
		return true
	}
	return refresh == RefreshUnfinalized && (!chain.HasFinalized || rec.Number > chain.Finalized)
}

// refClient retries transient failures (transport errors, timeouts, non-JSON
// or 5xx/429 bodies, rate-limit errors) with exponential backoff, so a slow
// or throttling reference does not turn verifiable blocks into unverified ones.
type refClient struct {
	c         *ethrpc.Client
	attempts  int
	baseDelay time.Duration
}

// callError is a failed reference call. Permanent means the node answered
// with an RPC error that retrying cannot change, such as a missing method.
type callError struct {
	reply     ethrpc.Reply
	permanent bool
}

func (e *callError) Error() string { return e.reply.AsError().Error() }

func (rc *refClient) call(ctx context.Context, method string, params ...any) (json.RawMessage, error) {
	delay := rc.baseDelay
	var last ethrpc.Reply
	for attempt := 0; attempt < max(rc.attempts, 1); attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
			delay = min(delay*2, 10*time.Second)
		}
		last = ethrpc.Parse(rc.c.Call(ctx, method, params...))
		switch {
		case last.Class == schema.ClassResult || last.Class == schema.ClassNull:
			return last.Result, nil
		case last.Class == schema.ClassRPCError && !rateLimited(last):
			return nil, &callError{reply: last, permanent: true}
		}
	}
	return nil, &callError{reply: last}
}

func rateLimited(r ethrpc.Reply) bool {
	if r.Code != nil && (*r.Code == -32005 || *r.Code == 429) {
		return true
	}
	msg := strings.ToLower(r.Message)
	return strings.Contains(msg, "rate limit") || strings.Contains(msg, "too many requests")
}

func fetchChain(ctx context.Context, c *refClient) (*chainRecord, error) {
	cr := &chainRecord{}
	if raw, err := c.call(ctx, "web3_clientVersion"); err == nil {
		_ = json.Unmarshal(raw, &cr.ClientVersion)
	}
	raw, err := c.call(ctx, "eth_chainId")
	if err != nil {
		return nil, err
	}
	id, err := ethrpc.ParseBigQuantity(raw)
	if err != nil || !id.IsUint64() {
		return nil, fmt.Errorf("eth_chainId returned %s", raw)
	}
	cr.ChainID = id.Uint64()
	if raw, err = c.call(ctx, "eth_blockNumber"); err != nil {
		return nil, err
	}
	if cr.Head, err = ethrpc.ParseQuantity(raw); err != nil {
		return nil, err
	}
	if raw, err := c.call(ctx, "eth_getBlockByNumber", "finalized", false); err == nil {
		if h, err := ethrpc.ParseHeader(raw); err == nil && h != nil {
			cr.Finalized, cr.HasFinalized = h.Number, true
		}
	}
	return cr, nil
}

func fetchBlock(ctx context.Context, c *refClient, b block) *refRecord {
	rec := &refRecord{Hash: b.hash, Number: b.number}
	fail := func(what string, err error) *refRecord {
		rec.FetchError = what + ": " + err.Error()
		return rec
	}
	raw, err := c.call(ctx, "eth_getBlockByNumber", ethrpc.Quantity(b.number), false)
	if err != nil {
		return fail("eth_getBlockByNumber", err)
	}
	if h, err := ethrpc.ParseHeader(raw); err == nil && h != nil {
		rec.CanonicalHash = ethrpc.HashHex(h.Hash)
	}
	raw, err = c.call(ctx, "eth_getBlockByHash", b.hash, false)
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
	raw, err = c.call(ctx, "eth_getBlockReceipts", b.hash)
	if err == nil && string(raw) != "null" {
		rec.Receipts, rec.ReceiptsSource = raw, "eth_getBlockReceipts"
		return rec
	}
	// Per-transaction receipts cost one call per transaction; use them only
	// when the node cannot serve block receipts, not after a transient failure.
	var ce *callError
	if err != nil && (!errors.As(err, &ce) || !ce.permanent) {
		return fail("eth_getBlockReceipts", err)
	}
	parts := make([]json.RawMessage, 0, len(h.TxHashes))
	for _, tx := range h.TxHashes {
		r, err := c.call(ctx, "eth_getTransactionReceipt", ethrpc.HashHex(tx))
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
