package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/internal/stubnode"
	"github.com/jsonrpc-bench/runner/types"
)

// An archive profile drives one RPC method through many parameter shapes and
// tells them apart only by call name. Keyed on the method alone the whole run
// collapses to one row, which is the dimension it was built to measure.
func TestCallBreakdownSeparatesCallsSharingOneMethod(t *testing.T) {
	srv := stubServer(t, stubnode.DefaultConfig())

	cfg := runConfig(srv.URL, []*config.Call{
		{Name: "proof_head", Method: "eth_getProof", Params: []any{"0x1", []any{}, "latest"}, Weight: 1},
		{Name: "proof_deep", Method: "eth_getProof", Params: []any{"0x1", []any{}, "0x4C4B40"}, Weight: 1},
	}, func(c *config.Config) {
		c.Duration = "2s"
		c.RPS = 100
		c.VUs = 20
	})

	result, _, err := Run(context.Background(), cfg, testOptions(t))
	require.NoError(t, err)

	client := result.ClientMetrics["stub"]
	require.Len(t, client.Methods, 1, "both calls issue the same method")
	require.Contains(t, client.Methods, "eth_getProof")

	require.Len(t, client.Calls, 2, "the call names are what tell the two apart")
	for _, name := range []string{"proof_head", "proof_deep"} {
		details := client.Calls[name]
		require.NotNil(t, details, name)
		assert.Equal(t, "eth_getProof", details.Method, "the method travels with the call")
		assert.NotZero(t, details.Count)
	}

	assert.EqualValues(t, client.Methods["eth_getProof"].Count,
		client.Calls["proof_head"].Count+client.Calls["proof_deep"].Count,
		"the per-call counts partition the method's total")
}

func clientWithCall(name, method string, p99 float64) map[string]*types.ClientMetrics {
	summary := types.MetricSummary{Count: 100, P99: p99}
	return map[string]*types.ClientMetrics{
		"stub": {
			Name:    "stub",
			Methods: map[string]types.MetricSummary{method: summary},
			Calls: map[string]*types.MethodMetrics{
				name: {MetricSummary: summary, Name: name, Method: method},
			},
		},
	}
}

// A threshold sits on a call, and its target is the call's name whenever the
// call declares no method of its own — as a file-driven call does. Resolving
// only against the per-method breakdown reported those as breaches for having
// no traffic.
func TestThresholdResolvesAgainstCallNames(t *testing.T) {
	client := clientWithCall("proof_head", "eth_getProof", 42)

	thresholds, err := CollectThresholds([]*callThresholds{
		{target: "proof_head", expressions: []string{"p(99)<1000"}},
	})
	require.NoError(t, err)

	breaches, err := EvaluateThresholds(thresholds, client)
	require.NoError(t, err)
	assert.Empty(t, breaches, "a threshold on a call that ran must resolve")
}

func TestThresholdOnAbsentCallIsStillABreach(t *testing.T) {
	client := clientWithCall("proof_head", "eth_getProof", 42)

	thresholds, err := CollectThresholds([]*callThresholds{
		{target: "proof_never_ran", expressions: []string{"p(99)<1000"}},
	})
	require.NoError(t, err)

	breaches, err := EvaluateThresholds(thresholds, client)
	require.NoError(t, err)
	require.Len(t, breaches, 1)
	assert.Contains(t, breaches[0].Expression, "no requests recorded")
}
