package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/internal/stubnode"
	"github.com/jsonrpc-bench/runner/types"
)

func probeTargetFor(t *testing.T, name string, node stubnode.Node) *target {
	t.Helper()
	cfg := stubnode.DefaultConfig()
	cfg.Node = node
	cfg.Default.Latency = &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 1}

	stub, err := stubnode.New(cfg)
	require.NoError(t, err)
	srv := httptest.NewServer(stub.Handler())
	t.Cleanup(srv.Close)

	tgt, err := newTarget(&types.ClientConfig{Name: name, Type: "nethermind", URL: srv.URL}, 2, DefaultTransportOptions())
	require.NoError(t, err)
	return tgt
}

func TestPreflightIdentifiesATarget(t *testing.T) {
	tgt := probeTargetFor(t, "nethermind", stubnode.Node{
		ClientVersion: "Nethermind/v1.31.0+abc/linux-x64/dotnet9.0.0",
		ChainID:       100,
		HeadBlock:     38_000_000,
	})

	info, err := Preflight(context.Background(), []*target{tgt}, PreflightOptions{})
	require.NoError(t, err)
	require.Len(t, info, 1)

	assert.Equal(t, "nethermind", info[0].Name)
	assert.Equal(t, "Nethermind/v1.31.0+abc/linux-x64/dotnet9.0.0", info[0].ClientVersion)
	assert.Equal(t, "100", info[0].ChainID, "the chain is recorded in decimal, not as the hex quantity")
	assert.EqualValues(t, 38_000_000, info[0].HeadBlock)
	assert.NotEmpty(t, info[0].HeadTimestamp)
	assert.Empty(t, info[0].ProbeErrors)

	healthy, reason := TargetHealth(info[0])
	assert.True(t, healthy, reason)
}

// Measuring a mainnet node against a Gnosis one produces two valid
// measurements of two different things, so it is refused unless asked for.
func TestPreflightRefusesAChainMismatch(t *testing.T) {
	mainnet := probeTargetFor(t, "mainnet", stubnode.Node{ClientVersion: "x", ChainID: 1, HeadBlock: 100})
	gnosis := probeTargetFor(t, "gnosis", stubnode.Node{ClientVersion: "x", ChainID: 100, HeadBlock: 100})

	_, err := Preflight(context.Background(), []*target{mainnet, gnosis}, PreflightOptions{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPreflight)
	assert.Contains(t, err.Error(), "different chains")
	assert.Contains(t, err.Error(), "--allow-chain-mismatch")

	_, err = Preflight(context.Background(), []*target{mainnet, gnosis}, PreflightOptions{AllowChainMismatch: true})
	assert.NoError(t, err)
}

func TestPreflightAcceptsMatchingChains(t *testing.T) {
	a := probeTargetFor(t, "a", stubnode.Node{ClientVersion: "x", ChainID: 1, HeadBlock: 100})
	b := probeTargetFor(t, "b", stubnode.Node{ClientVersion: "y", ChainID: 1, HeadBlock: 101})

	info, err := Preflight(context.Background(), []*target{a, b}, PreflightOptions{})
	require.NoError(t, err)
	assert.Len(t, info, 2)
}

// Failing here costs a second; discovering it after a five-minute run costs the
// run.
func TestPreflightFailsFastOnAnUnreachableTarget(t *testing.T) {
	tgt, err := newTarget(&types.ClientConfig{Name: "dead", URL: "http://127.0.0.1:1"}, 2, DefaultTransportOptions())
	require.NoError(t, err)

	start := time.Now()
	_, err = Preflight(context.Background(), []*target{tgt}, PreflightOptions{Timeout: 2 * time.Second})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPreflight)
	assert.Contains(t, err.Error(), "dead")
	assert.Less(t, time.Since(start), 5*time.Second)
}

func TestPreflightFailsWhenProbesAreRejected(t *testing.T) {
	tgt := probeTargetFor(t, "sick", stubnode.Node{ChainID: 1, HeadBlock: 1, ProbeError: true})

	_, err := Preflight(context.Background(), []*target{tgt}, PreflightOptions{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPreflight)
}

func TestTargetHealth(t *testing.T) {
	cases := map[string]struct {
		info    types.ClientProvenance
		healthy bool
		reason  string
	}{
		"synced and current": {
			info:    types.ClientProvenance{HeadBlock: 100, HeadTimestamp: "t", HeadAgeSeconds: 6},
			healthy: true,
		},
		"still syncing": {
			info:    types.ClientProvenance{HeadBlock: 100, Syncing: true},
			healthy: false, reason: "syncing",
		},
		"head has stopped moving": {
			info:    types.ClientProvenance{HeadBlock: 100, HeadTimestamp: "t", HeadAgeSeconds: 3600},
			healthy: false, reason: "old",
		},
		"head unknown": {
			info:    types.ClientProvenance{ClientVersion: "x"},
			healthy: false, reason: "unknown",
		},
		// A gap in the provenance is reported separately: not knowing the head
		// age is not evidence that the node is unfit.
		"probe gap but otherwise current": {
			info:    types.ClientProvenance{HeadBlock: 100, ProbeErrors: []string{"eth_syncing: HTTP 500"}},
			healthy: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			healthy, reason := TargetHealth(tc.info)
			assert.Equal(t, tc.healthy, healthy, reason)
			if tc.reason != "" {
				assert.Contains(t, reason, tc.reason)
			}
		})
	}
}

func TestRunRecordsProvenanceFromPreflight(t *testing.T) {
	srv := stubServer(t, func() stubnode.Config {
		cfg := stubnode.DefaultConfig()
		cfg.Node = stubnode.Node{ClientVersion: "Nethermind/v1.31.0", ChainID: 1, HeadBlock: 21_000_000}
		cfg.Default.Latency = &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 1}
		return cfg
	}())

	cfg := runConfig(srv.URL, []*config.Call{
		{Name: "eth_call", Method: "eth_call", Params: []any{}, Weight: 1},
	}, func(c *config.Config) { c.Duration = "1s"; c.RPS = 20; c.VUs = 5 })

	result, _, err := Run(context.Background(), cfg, testOptions(t))
	require.NoError(t, err)

	require.Len(t, result.Manifest.Clients, 1)
	client := result.Manifest.Clients[0]
	assert.Equal(t, "Nethermind/v1.31.0", client.ClientVersion)
	assert.Equal(t, "1", client.ChainID)
	assert.EqualValues(t, 21_000_000, client.HeadBlock)
	assert.False(t, result.Manifest.PreflightSkipped)
}

// Skipping the check is allowed, but the manifest has to say so: the
// provenance is then only what the config claimed, not what answered.
func TestRunRecordsThatPreflightWasSkipped(t *testing.T) {
	srv := stubServer(t, stubnode.Config{
		Default: stubnode.Method{Latency: &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 1}},
	})

	cfg := runConfig(srv.URL, []*config.Call{
		{Name: "eth_call", Method: "eth_call", Params: []any{}, Weight: 1},
	}, func(c *config.Config) { c.Duration = "1s"; c.RPS = 20; c.VUs = 5 })

	opts := testOptions(t)
	opts.SkipPreflight = true

	result, _, err := Run(context.Background(), cfg, opts)
	require.NoError(t, err)

	assert.True(t, result.Manifest.PreflightSkipped)
	require.Len(t, result.Manifest.Clients, 1)
	assert.Empty(t, result.Manifest.Clients[0].ClientVersion)
	assert.Equal(t, srv.URL, result.Manifest.Clients[0].URL)
}

func TestParseHexUint(t *testing.T) {
	for input, want := range map[string]uint64{"0x0": 0, "0x64": 100, "0x1406f40": 21000000, "64": 100} {
		got, err := parseHexUint(input)
		require.NoError(t, err, input)
		assert.Equal(t, want, got, input)
	}
	for _, input := range []string{"", "0x", "latest", "0xzz"} {
		_, err := parseHexUint(input)
		assert.Error(t, err, input)
	}
}

// The head timestamp comes from the node being measured. A seconds count past
// what time.Unix can hold would wrap to a date far in the past or the future,
// and the staleness check would then be reading a number the node chose rather
// than the time — so it is reported as a probe failure instead of a time.
func TestPreflightRejectsAnUnrepresentableHeadTimestamp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		result := map[string]any{}
		switch req.Method {
		case "web3_clientVersion":
			writeResult(w, req.ID, "Nethermind/v1.0.0")
			return
		case "eth_chainId":
			writeResult(w, req.ID, "0x1")
			return
		case "eth_blockNumber":
			writeResult(w, req.ID, "0x1")
			return
		case "eth_syncing":
			writeResult(w, req.ID, false)
			return
		case "eth_getBlockByNumber":
			// Larger than math.MaxInt64.
			result = map[string]any{"timestamp": "0xffffffffffffffff"}
		}
		writeResult(w, req.ID, result)
	}))
	t.Cleanup(srv.Close)

	tgt, err := newTarget(&types.ClientConfig{Name: "n", URL: srv.URL}, 2, DefaultTransportOptions())
	require.NoError(t, err)

	info, err := Preflight(context.Background(), []*target{tgt}, PreflightOptions{})
	require.NoError(t, err)
	require.Len(t, info, 1)

	assert.Empty(t, info[0].HeadTimestamp, "no time is recorded rather than a fabricated one")
	require.NotEmpty(t, info[0].ProbeErrors)
	assert.Contains(t, strings.Join(info[0].ProbeErrors, " "), "not a representable time")
}

func writeResult(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}
