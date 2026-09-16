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

// The two failures the previous pipeline could not report have to be visible in
// the result every consumer reads — the exports, the HTML report and the
// historic store all project from this struct.

func TestResultReportsRPCErrorsPerOutcomeClass(t *testing.T) {
	srv := stubServer(t, stubnode.Config{
		Seed:    61,
		Default: stubnode.Method{Latency: &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 1}},
		Methods: map[string]stubnode.Method{
			"eth_call":    {RPCErrorRate: 1, RPCErrorCode: -32000},
			"eth_getLogs": {NullResultRate: 1},
		},
	})

	cfg := runConfig(srv.URL, []*config.Call{
		{Name: "eth_call", Method: "eth_call", Params: []any{}, Weight: 50},
		{Name: "eth_getLogs", Method: "eth_getLogs", Params: []any{}, Weight: 50},
	}, func(c *config.Config) { c.Duration = "1s"; c.RPS = 60; c.VUs = 10 })

	result, _, err := Run(context.Background(), cfg, testOptions(t))
	require.NoError(t, err)

	client := result.ClientMetrics["stub"]
	require.NotZero(t, client.TotalRequests)

	// Every class is counted, successes included, so "fast because it returned
	// nothing" is visible rather than folded into ok.
	assert.NotZero(t, client.Outcomes["rpc_error"])
	assert.NotZero(t, client.Outcomes["rpc_null"])
	assert.Zero(t, client.Outcomes["ok"], "one method errored and the other returned null")

	// Per method, which is the diagnostic question: which method is failing.
	require.NotNil(t, client.MethodDetails["eth_call"])
	assert.NotZero(t, client.MethodDetails["eth_call"].Outcomes["rpc_error"])
	assert.Zero(t, client.MethodDetails["eth_call"].Outcomes["rpc_null"])
	assert.NotZero(t, client.MethodDetails["eth_getLogs"].Outcomes["rpc_null"])

	// A null result is a success, so it must not inflate the error rate.
	assert.Greater(t, client.ErrorRate, 0.0)
	assert.Less(t, client.ErrorRate, 100.0)
}

func TestResultReportsUnderDeliveredLoad(t *testing.T) {
	srv := stubServer(t, stubnode.Config{
		Seed:    67,
		Default: stubnode.Method{Latency: &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 50}},
	})

	cfg := runConfig(srv.URL, []*config.Call{
		{Name: "eth_call", Method: "eth_call", Params: []any{}, Weight: 1},
	}, func(c *config.Config) { c.Duration = "1s"; c.RPS = 100; c.VUs = 1 })

	opts := testOptions(t)
	opts.Saturation = SaturationDrop

	result, _, err := Run(context.Background(), cfg, opts)
	require.NoError(t, err)

	d := result.ClientMetrics["stub"].Delivery
	assert.EqualValues(t, 100, d.Scheduled, "100 rps for 1s")
	assert.Less(t, d.Sent, d.Scheduled)
	assert.NotZero(t, d.Dropped)
	assert.Equal(t, d.Scheduled, d.Sent+d.Dropped, "every scheduled request is accounted for")

	assert.EqualValues(t, 100, d.TargetRPS)
	assert.Less(t, d.AchievedRPS, 100.0)
	assert.Less(t, d.DeliveryRatio(), 1.0)
	assert.False(t, d.Complete(), "a run that dropped requests is not a complete delivery")
}

func TestResultReportsDispatchDelayUnderQueueing(t *testing.T) {
	srv := stubServer(t, stubnode.Config{
		Seed:    71,
		Default: stubnode.Method{Latency: &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 50}},
	})

	cfg := runConfig(srv.URL, []*config.Call{
		{Name: "eth_call", Method: "eth_call", Params: []any{}, Weight: 1},
	}, func(c *config.Config) { c.Duration = "1s"; c.RPS = 100; c.VUs = 1 })

	opts := testOptions(t)
	opts.Saturation = SaturationQueue

	result, _, err := Run(context.Background(), cfg, opts)
	require.NoError(t, err)

	d := result.ClientMetrics["stub"].Delivery
	assert.NotZero(t, d.Late)
	assert.Greater(t, d.MaxDispatchDelayMs, 0.0,
		"the dispatch delay is what a saturated run hides when it is not recorded")
	assert.False(t, d.Complete())
}

func TestResultDeliveryIsCompleteOnAHealthyRun(t *testing.T) {
	srv := stubServer(t, stubnode.Config{
		Seed:    73,
		Default: stubnode.Method{Latency: &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 2}},
	})

	cfg := runConfig(srv.URL, []*config.Call{
		{Name: "eth_call", Method: "eth_call", Params: []any{}, Weight: 1},
	}, func(c *config.Config) { c.Duration = "2s"; c.RPS = 40; c.VUs = 10 })

	result, _, err := Run(context.Background(), cfg, testOptions(t))
	require.NoError(t, err)

	d := result.ClientMetrics["stub"].Delivery
	assert.True(t, d.Complete(), "scheduled=%d sent=%d late=%d dropped=%d", d.Scheduled, d.Sent, d.Late, d.Dropped)
	assert.EqualValues(t, 1, int(d.DeliveryRatio()))
	assert.NotZero(t, d.InflightPeak)
	assert.Greater(t, d.ElapsedSeconds, 0.0)
}

// A comparison that cannot say which semantics produced it invites reading a
// semantics change as a regression, since the error rate now counts JSON-RPC
// errors an HTTP-only pipeline could not see.
func TestResultRecordsHowTheRunWasProduced(t *testing.T) {
	srv := stubServer(t, stubnode.Config{
		Seed:    79,
		Default: stubnode.Method{Latency: &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 1}},
	})

	cfg := runConfig(srv.URL, []*config.Call{
		{Name: "eth_call", Method: "eth_call", Params: []any{}, Weight: 1},
	}, func(c *config.Config) { c.Duration = "1s"; c.RPS = 20; c.VUs = 5; c.Seed = 4242 })

	opts := testOptions(t)
	opts.Saturation = SaturationDrop
	opts.Transport.AcceptCompression = true

	result, _, err := Run(context.Background(), cfg, opts)
	require.NoError(t, err)

	m := result.Manifest
	assert.Equal(t, "native", m.Engine)
	assert.Equal(t, Version, m.EngineVersion)
	// Against the constant the comparability gate reads, not the engine's own
	// name for it: a second literal that drifted would make every stored run
	// incomparable with every new one.
	assert.Equal(t, types.ErrorRateSemanticsRPCAware, m.ErrorRateSemantics)
	assert.EqualValues(t, 4242, m.Seed)
	assert.Equal(t, string(SaturationDrop), m.Saturation)
	assert.Equal(t, 20, m.TargetRPS)
	assert.Equal(t, 5, m.Concurrency)
	assert.True(t, m.AcceptCompression, "a transport setting that changes the numbers must be recorded")
	assert.True(t, m.ReuseConnections)
	assert.False(t, m.HTTP2)
	assert.NotEmpty(t, m.StartTime)

	require.Len(t, m.Clients, 1)
	assert.Equal(t, "stub", m.Clients[0].Name)
	assert.Equal(t, srv.URL, m.Clients[0].URL)
}
