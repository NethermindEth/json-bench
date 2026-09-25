package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/jsonrpc-bench/runner/freshness/ethrpc"
	"github.com/jsonrpc-bench/runner/freshness/schema"
)

// maxSlotSeconds bounds a slot duration from config or the beacon spec well
// below time.Duration overflow; real networks use 2-12 s.
const maxSlotSeconds = 3600

// futureOffset is how far past head the preflight asks, to capture the
// node's own "block unknown" answer for each probe method.
const futureOffset = 10000

// unknownHash stands in for a block/transaction no node can know.
var unknownHash = common.HexToHash("0x00000000000000000000000000000000000000000000000000000000deadbeef")

func (r *Runner) preflight(ctx context.Context) error {
	caps := &schema.Capabilities{
		SchemaVersion: schema.Version,
		RunID:         r.env.RunID,
		PairID:        r.env.PairID,
		Probes:        map[string]*schema.ProbeCapability{},
	}
	r.caps = caps

	if raw, err := r.el.CallResult(ctx, "web3_clientVersion"); err == nil {
		_ = json.Unmarshal(raw, &r.manifest.ELClientVersion)
	}

	raw, err := r.el.CallResult(ctx, "eth_chainId")
	if err != nil {
		return fmt.Errorf("eth_chainId: %w", err)
	}
	chainID, err := ethrpc.ParseBigQuantity(raw)
	if err != nil || !chainID.IsUint64() {
		return fmt.Errorf("eth_chainId returned %s", raw)
	}
	caps.ChainID = chainID.Uint64()

	raw, err = r.el.CallResult(ctx, "eth_getBlockByNumber", "0x0", false)
	switch h, perr := ethrpc.ParseHeader(raw); {
	case err != nil:
		caps.GenesisError = truncate(err.Error(), 200)
	case perr != nil:
		caps.GenesisError = truncate(perr.Error(), 200)
	case h == nil:
		caps.GenesisError = "block 0 returned null"
	default:
		caps.GenesisHash = ethrpc.HashHex(h.Hash)
	}
	if caps.GenesisHash == "" {
		r.log.Warnf("genesis block unavailable (%s), usually because the node pruned history; chain id is still checked, but review cannot confirm genesis identity", caps.GenesisError)
	}

	raw, err = r.el.CallResult(ctx, "eth_syncing")
	if err != nil {
		return fmt.Errorf("eth_syncing: %w", err)
	}
	var syncing bool
	if json.Unmarshal(raw, &syncing) == nil {
		caps.ELSyncing = &syncing
		if syncing {
			return fmt.Errorf("execution client reports eth_syncing != false")
		}
	} else {
		t := true
		caps.ELSyncing = &t
		return fmt.Errorf("execution client is syncing: %s", truncate(string(raw), 200))
	}

	head, err := r.fetchHeader(ctx, "latest")
	if err != nil || head == nil {
		return fmt.Errorf("latest block: %v", err)
	}

	if raw, err := r.el.CallResult(ctx, "eth_getCode", ethrpc.HistoryContract, "latest"); err == nil {
		var code string
		_ = json.Unmarshal(raw, &code)
		caps.HistoryCode = len(strings.TrimPrefix(code, "0x")) > 0
	}
	if caps.HistoryCode {
		if raw, err := r.el.CallResult(ctx, "eth_call", historyCall(head.Number-1), ethrpc.Quantity(head.Number)); err == nil {
			if got, err := ethrpc.CanonHash(raw); err == nil && got == ethrpc.HashHex(head.ParentHash) {
				caps.HistoryCanary = true
			}
		}
	}

	for _, p := range r.cfg.EnabledProbes() {
		caps.Probes[p] = r.probeCapability(ctx, p, head, caps)
	}

	if r.beacon != nil {
		if err := r.beaconPreflight(ctx, caps); err != nil {
			return err
		}
	}

	if err := r.resolveSlotDuration(ctx, caps.ChainID); err != nil {
		return err
	}

	r.manifest.ChainID = caps.ChainID
	r.manifest.GenesisHash = caps.GenesisHash
	for _, p := range r.cfg.EnabledProbes() {
		if c := caps.Probes[p]; c.Supported {
			r.probes = append(r.probes, p)
			r.sigs[p] = c.NotReady
		} else {
			r.log.Warnf("probe %s unsupported: %s", p, c.Reason)
		}
	}
	if len(r.probes) == 0 {
		return fmt.Errorf("no enabled probe passed preflight")
	}
	return nil
}

func (r *Runner) probeCapability(ctx context.Context, probe string, head *ethrpc.Header, caps *schema.Capabilities) *schema.ProbeCapability {
	c := &schema.ProbeCapability{}
	if strings.HasPrefix(probe, "state_") && !(caps.HistoryCode && caps.HistoryCanary) {
		c.Reason = "EIP-2935 history contract missing or canary returned an unexpected value"
		return c
	}

	var tx *common.Hash
	if probe == schema.ProbeTransactionReceipt {
		sample, err := r.headerWithTransactions(ctx, head)
		if err != nil {
			c.Reason = err.Error()
			return c
		}
		head = sample
		tx = &sample.TxHashes[len(sample.TxHashes)-1]
	}

	method, params := request(probe, head.Number, head.Hash, tx)
	if probe == schema.ProbeStateLatest {
		// "latest" may have advanced since head was read; the canary for the
		// actual latest block is not known here, so only check the shape.
		method, params = request(schema.ProbeStateNumber, head.Number, head.Hash, nil)
	}
	reply := ethrpc.Parse(r.el.Call(ctx, method, params...))
	if reply.Class != schema.ClassResult {
		c.Reason = fmt.Sprintf("%s on head: %v", method, reply.AsError())
		return c
	}
	cand, err := ethrpc.Canonicalize(probe, reply.Result)
	if err != nil {
		c.Reason = fmt.Sprintf("%s on head: unparseable result: %v", method, err)
		return c
	}
	parent := head.ParentHash
	if verdict, why := ethrpc.Check(probe, cand, ethrpc.Expectation{ParentHash: &parent, Header: head, TxHash: tx}); verdict != schema.LocalMatch {
		c.Reason = fmt.Sprintf("%s on head failed the local check: %s", method, why)
		return c
	}

	fmethod, fparams := r.futureRequest(probe, head.Number)
	fr := ethrpc.Parse(r.el.Call(ctx, fmethod, fparams...))
	switch fr.Class {
	case schema.ClassTransport, schema.ClassTimeout, schema.ClassHTTPError, schema.ClassBadBody:
		c.Reason = fmt.Sprintf("not-ready probe failed at transport level: %v", fr.AsError())
		return c
	}
	c.NotReady = ethrpc.Signature(fr)
	if c.NotReady == nil {
		c.Reason = "node returned data for a block it cannot have"
		return c
	}
	c.Supported = true
	return c
}

// futureRequest asks for data the node cannot have yet: a block far past
// head, or a hash no block has.
func (r *Runner) futureRequest(probe string, head uint64) (string, []any) {
	future := head + futureOffset
	switch probe {
	case schema.ProbeStateNumber, schema.ProbeLogsNumber:
		return request(probe, future, common.Hash{}, nil)
	case schema.ProbeStateLatest:
		return "eth_call", []any{historyCall(future), "latest"}
	case schema.ProbeTransactionReceipt:
		return request(probe, future, unknownHash, &unknownHash)
	default:
		return request(probe, future, unknownHash, nil)
	}
}

// headerWithTransactions walks back from head to the nearest block with a
// transaction, for the receipt probe preflight.
func (r *Runner) headerWithTransactions(ctx context.Context, head *ethrpc.Header) (*ethrpc.Header, error) {
	h := head
	for i := 0; i < 16 && h != nil; i++ {
		if len(h.TxHashes) > 0 {
			return h, nil
		}
		var err error
		h, err = r.fetchHeader(ctx, ethrpc.Quantity(h.Number-1))
		if err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("no transaction in the last 16 blocks to test receipts with")
}

func (r *Runner) beaconPreflight(ctx context.Context, caps *schema.Capabilities) error {
	data, checks, err := r.beacon.syncStatus(ctx)
	if err != nil {
		return fmt.Errorf("beacon /eth/v1/node/syncing: %w", err)
	}
	caps.CLSync = data
	caps.CLSyncChecks = checks
	for field, verdict := range checks {
		if verdict == "failed" {
			return fmt.Errorf("consensus client unhealthy: %s", field)
		}
		if verdict == "unknown" {
			r.log.Warnf("consensus client does not report %s; recorded as unknown", field)
		}
	}
	r.manifest.CLClientVersion = r.beacon.version(ctx)
	if g, err := r.beacon.genesisTime(ctx); err == nil {
		r.manifest.BeaconGenesisTime = &g
	}
	if spe, err := r.beacon.specValue(ctx, "SLOTS_PER_EPOCH"); err == nil && spe > 0 {
		r.manifest.SlotsPerEpoch = &spe
	}
	return nil
}

func (r *Runner) resolveSlotDuration(ctx context.Context, chainID uint64) error {
	switch {
	case r.cfg.Chain.SlotDurationSeconds > 0:
		r.manifest.SlotDurationSeconds, r.manifest.SlotDurationSource = r.cfg.Chain.SlotDurationSeconds, "config"
	case r.beacon != nil:
		if s, err := r.beacon.specValue(ctx, "SECONDS_PER_SLOT"); err == nil && s > 0 {
			r.manifest.SlotDurationSeconds, r.manifest.SlotDurationSource = s, "beacon_spec"
		}
	}
	if r.manifest.SlotDurationSeconds == 0 {
		if s, ok := chainSlotPresets[chainID]; ok {
			r.manifest.SlotDurationSeconds, r.manifest.SlotDurationSource = s, "chain_preset"
		}
	}
	if r.manifest.SlotDurationSeconds == 0 {
		return fmt.Errorf("slot duration unknown for chain %d: set chain.slot_duration_seconds or pair.cl.beacon_url", chainID)
	}
	if r.manifest.SlotDurationSeconds > maxSlotSeconds {
		return fmt.Errorf("slot duration %d s (%s) is implausible; the limit is %d s", r.manifest.SlotDurationSeconds, r.manifest.SlotDurationSource, maxSlotSeconds)
	}
	r.slotDur = time.Duration(r.manifest.SlotDurationSeconds) * time.Second
	return nil
}

func (r *Runner) fetchHeader(ctx context.Context, tag string) (*ethrpc.Header, error) {
	raw, err := r.el.CallResult(ctx, "eth_getBlockByNumber", tag, false)
	if err != nil {
		return nil, err
	}
	return ethrpc.ParseHeader(raw)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
