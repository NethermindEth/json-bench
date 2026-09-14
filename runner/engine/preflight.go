package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jsonrpc-bench/runner/types"
)

// staleHeadThreshold is when a head stops looking current. Ethereum produces a
// block every 12s and Gnosis every 5s, so a couple of minutes behind is either
// a stalled node or one still catching up — either way its latency describes
// something other than a healthy node serving current state.
const staleHeadThreshold = 2 * time.Minute

// ErrPreflight is returned when a target cannot be benchmarked, or when the
// targets are not comparable to each other.
var ErrPreflight = errors.New("preflight check failed")

// PreflightOptions controls how strict the check is.
type PreflightOptions struct {
	// AllowChainMismatch permits targets on different chains. Off by default:
	// comparing a mainnet node against a testnet one produces two valid
	// measurements of two different things.
	AllowChainMismatch bool

	Timeout time.Duration
}

// Preflight establishes what each target actually is before any load is
// applied, and refuses to proceed when the answer makes the run meaningless.
//
// A benchmark result is not citable without this. "Nethermind is slower on
// eth_getLogs" means nothing without the version, the chain, and whether the
// node was synced when it was asked.
func Preflight(ctx context.Context, targets []*target, opts PreflightOptions) ([]types.ClientProvenance, error) {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	out := make([]types.ClientProvenance, len(targets))
	var wg sync.WaitGroup
	for i, tgt := range targets {
		wg.Add(1)
		go func(i int, tgt *target) {
			defer wg.Done()
			probeCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			out[i] = probeTarget(probeCtx, tgt)
		}(i, tgt)
	}
	wg.Wait()

	var unreachable []string
	for _, info := range out {
		if info.ClientVersion == "" && info.ChainID == "" {
			unreachable = append(unreachable, fmt.Sprintf("%s (%s)", info.Name, strings.Join(info.ProbeErrors, "; ")))
		}
	}
	if len(unreachable) > 0 {
		return out, fmt.Errorf("%w: no usable response from %s", ErrPreflight, strings.Join(unreachable, ", "))
	}

	if !opts.AllowChainMismatch {
		if err := requireOneChain(out); err != nil {
			return out, err
		}
	}

	return out, nil
}

// requireOneChain refuses a run whose targets are not on the same chain.
func requireOneChain(infos []types.ClientProvenance) error {
	chains := make(map[string][]string)
	for _, info := range infos {
		if info.ChainID != "" {
			chains[info.ChainID] = append(chains[info.ChainID], info.Name)
		}
	}
	if len(chains) < 2 {
		return nil
	}

	ids := make([]string, 0, len(chains))
	for id := range chains {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		sort.Strings(chains[id])
		parts = append(parts, fmt.Sprintf("chain %s: %s", id, strings.Join(chains[id], ", ")))
	}
	return fmt.Errorf("%w: targets are on different chains (%s); pass --allow-chain-mismatch to measure them anyway",
		ErrPreflight, strings.Join(parts, " | "))
}

func probeTarget(ctx context.Context, tgt *target) types.ClientProvenance {
	info := types.ClientProvenance{Name: tgt.name, Type: tgt.clientType, URL: tgt.url}

	if version, err := probeString(ctx, tgt, "web3_clientVersion"); err != nil {
		info.ProbeErrors = append(info.ProbeErrors, "web3_clientVersion: "+err.Error())
	} else {
		info.ClientVersion = version
	}

	if chain, err := probeString(ctx, tgt, "eth_chainId"); err != nil {
		info.ProbeErrors = append(info.ProbeErrors, "eth_chainId: "+err.Error())
	} else if n, err := parseHexUint(chain); err != nil {
		info.ProbeErrors = append(info.ProbeErrors, "eth_chainId: "+err.Error())
	} else {
		info.ChainID = strconv.FormatUint(n, 10)
	}

	head, err := probeString(ctx, tgt, "eth_blockNumber")
	if err != nil {
		info.ProbeErrors = append(info.ProbeErrors, "eth_blockNumber: "+err.Error())
	} else if n, err := parseHexUint(head); err != nil {
		info.ProbeErrors = append(info.ProbeErrors, "eth_blockNumber: "+err.Error())
	} else {
		info.HeadBlock = n
		if ts, err := probeHeadTimestamp(ctx, tgt, head); err != nil {
			info.ProbeErrors = append(info.ProbeErrors, "eth_getBlockByNumber: "+err.Error())
		} else {
			info.HeadTimestamp = ts.UTC().Format(time.RFC3339)
			info.HeadAgeSeconds = time.Since(ts).Seconds()
		}
	}

	if syncing, err := probeSyncing(ctx, tgt); err != nil {
		info.ProbeErrors = append(info.ProbeErrors, "eth_syncing: "+err.Error())
	} else {
		info.Syncing = syncing
	}

	return info
}

// TargetHealth reports whether a target looks fit to measure. A syncing node or
// one whose head has stopped moving will produce numbers, and they will describe
// something other than a healthy node serving current state.
//
// A probe that did not answer is reported separately: not knowing the head age
// is a gap in the provenance, not evidence that the node is unfit.
func TargetHealth(info types.ClientProvenance) (healthy bool, reason string) {
	switch {
	case info.Syncing:
		return false, "node is still syncing"
	case info.HeadBlock == 0:
		return false, "head block is unknown, so it cannot be told whether the node is keeping up"
	case info.HeadTimestamp != "" && info.HeadAgeSeconds > staleHeadThreshold.Seconds():
		return false, fmt.Sprintf("head block is %.0fs old", info.HeadAgeSeconds)
	}
	return true, ""
}

func probeCall(ctx context.Context, tgt *target, method string, params []any) (json.RawMessage, error) {
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": method, "params": params,
	})
	if err != nil {
		return nil, err
	}

	res := tgt.do(ctx, payload)
	if res.err != nil {
		return nil, res.err
	}
	if res.status != 200 {
		return nil, fmt.Errorf("HTTP %d", res.status)
	}

	var env rpcEnvelope
	if err := json.Unmarshal(res.body, &env); err != nil {
		return nil, fmt.Errorf("unparseable response")
	}
	if env.Error != nil {
		return nil, fmt.Errorf("JSON-RPC error %d: %s", env.Error.Code, env.Error.Message)
	}
	if env.Result == nil {
		return nil, fmt.Errorf("no result")
	}
	return env.Result, nil
}

func probeString(ctx context.Context, tgt *target, method string) (string, error) {
	raw, err := probeCall(ctx, tgt, method, []any{})
	if err != nil {
		return "", err
	}
	var out string
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("expected a string, got %s", truncate(string(raw), 40))
	}
	return out, nil
}

func probeHeadTimestamp(ctx context.Context, tgt *target, head string) (time.Time, error) {
	raw, err := probeCall(ctx, tgt, "eth_getBlockByNumber", []any{head, false})
	if err != nil {
		return time.Time{}, err
	}
	var block struct {
		Timestamp string `json:"timestamp"`
	}
	if err := json.Unmarshal(raw, &block); err != nil || block.Timestamp == "" {
		return time.Time{}, fmt.Errorf("block has no timestamp")
	}
	seconds, err := parseHexUint(block.Timestamp)
	if err != nil {
		return time.Time{}, err
	}
	// The value comes from the node being measured, and a seconds count past
	// this cannot be held in the signed type time.Unix takes: it would wrap to a
	// date far in the past or the future, and the head-staleness check would
	// then be reading a number the node effectively chose.
	if seconds > math.MaxInt64 {
		return time.Time{}, fmt.Errorf("block timestamp %s is not a representable time", block.Timestamp)
	}
	return time.Unix(int64(seconds), 0), nil
}

// probeSyncing reads eth_syncing, which answers false when synced and an object
// describing the progress when not.
func probeSyncing(ctx context.Context, tgt *target) (bool, error) {
	raw, err := probeCall(ctx, tgt, "eth_syncing", []any{})
	if err != nil {
		return false, err
	}
	var synced bool
	if err := json.Unmarshal(raw, &synced); err == nil {
		return synced, nil
	}
	return true, nil
}

func parseHexUint(v string) (uint64, error) {
	n, err := strconv.ParseUint(strings.TrimPrefix(v, "0x"), 16, 64)
	if err != nil {
		return 0, fmt.Errorf("expected a hex quantity, got %q", truncate(v, 40))
	}
	return n, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
