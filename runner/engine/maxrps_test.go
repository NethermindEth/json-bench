package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/internal/stubnode"
	"github.com/jsonrpc-bench/runner/types"
)

// cappedNode models a node that serves `workers` requests at a time, each
// taking serviceMs. Its capacity is workers/serviceMs requests a second, which
// gives the search a known answer to find.
func cappedNode(t *testing.T, workers int, serviceMs float64) string {
	t.Helper()
	cfg := stubnode.DefaultConfig()
	cfg.Node.ConcurrencyLimit = workers
	cfg.Default.Latency = &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: serviceMs}
	return stubServer(t, cfg).URL
}

func searchConfig(url string, vus int) *config.Config {
	return runConfig(url, []*config.Call{
		{Name: "eth_call", Method: "eth_call", Params: []any{}, Weight: 1},
	}, func(c *config.Config) {
		c.TestName = "capacity-search"
		c.RPS = 10
		c.VUs = vus
	})
}

func fastSearch(slo SLO) SearchOptions {
	return SearchOptions{
		SLO:           slo,
		MinRPS:        50,
		ProbeDuration: 700 * time.Millisecond,
		Tolerance:     40,
		Settle:        0,
	}
}

func TestFindMaxRPSRecoversAKnownCapacity(t *testing.T) {
	// 4 workers at 20ms is 200 requests a second.
	url := cappedNode(t, 4, 20)
	cfg := searchConfig(url, 4000)

	result, err := FindMaxRPS(context.Background(), cfg, testOptions(t),
		fastSearch(SLO{P99Ms: 150, ErrorRatePercent: 1, MinDeliveredPercent: 95}))
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.True(t, result.Conclusive(), "the search was bounded by %s", result.LimitedBy)
	assert.Equal(t, "slo", result.LimitedBy)
	assert.InDelta(t, 200, result.MaxRPS, 100, "the answer should be near the node's real capacity")
	assert.Greater(t, result.LowestFailingRPS, result.MaxRPS, "the answer must be bracketed from above")
	assert.NotEmpty(t, result.Probes)
}

// The failure this exists to avoid: at a rate the generator cannot offer, the
// latency looks terrible, and a search that only read latency would report the
// generator's ceiling as the node's capacity.
func TestFindMaxRPSRefusesToBlameTheEndpointForAGeneratorLimit(t *testing.T) {
	url := cappedNode(t, 2, 40)
	// One slot cannot offer even 50 rps against a 40ms service time.
	cfg := searchConfig(url, 1)

	result, err := FindMaxRPS(context.Background(), cfg, testOptions(t),
		fastSearch(SLO{P99Ms: 100, ErrorRatePercent: 1, MinDeliveredPercent: 95}))
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.False(t, result.Conclusive())
	assert.Equal(t, "generator", result.LimitedBy)
	assert.Contains(t, result.Explain(), "Raise vus")

	require.NotEmpty(t, result.Probes)
	last := result.Probes[len(result.Probes)-1]
	assert.Equal(t, VerdictGeneratorLimited, last.Verdict)
	assert.Less(t, last.DeliveredPct, 95.0)
}

func TestFindMaxRPSStopsAtTheSearchCeiling(t *testing.T) {
	url := cappedNode(t, 64, 2)
	cfg := searchConfig(url, 500)

	search := fastSearch(SLO{P99Ms: 5000, ErrorRatePercent: 50, MinDeliveredPercent: 80})
	search.MaxRPS = 100

	result, err := FindMaxRPS(context.Background(), cfg, testOptions(t), search)
	require.NoError(t, err)

	assert.Equal(t, "search_ceiling", result.LimitedBy)
	assert.Equal(t, 100, result.MaxRPS)
	assert.Zero(t, result.LowestFailingRPS, "no failing rate was found, so the answer is a floor")
	assert.Contains(t, result.Explain(), "floor rather than a limit")
}

// An SLO nothing can meet must produce no answer rather than the lowest rate
// tried.
func TestFindMaxRPSReportsWhenEvenTheLowestRateFails(t *testing.T) {
	url := cappedNode(t, 8, 30)
	cfg := searchConfig(url, 200)

	result, err := FindMaxRPS(context.Background(), cfg, testOptions(t),
		fastSearch(SLO{P99Ms: 1, ErrorRatePercent: 1, MinDeliveredPercent: 90}))
	require.NoError(t, err)

	assert.Zero(t, result.MaxRPS)
	assert.NotZero(t, result.LowestFailingRPS)
	assert.Contains(t, result.Explain(), "below")
}

func TestFindMaxRPSRecordsWhatItMeasured(t *testing.T) {
	url := cappedNode(t, 8, 10)
	cfg := searchConfig(url, 400)

	result, err := FindMaxRPS(context.Background(), cfg, testOptions(t),
		fastSearch(SLO{P99Ms: 200, ErrorRatePercent: 1, MinDeliveredPercent: 95}))
	require.NoError(t, err)

	m := result.Manifest
	assert.Equal(t, "native", m.Engine)
	assert.Equal(t, ErrorRateSemanticsRPCAware, m.ErrorRateSemantics)
	assert.Equal(t, string(SaturationDrop), m.Saturation,
		"probes drop rather than queue, so a shortfall stays countable")
	assert.Zero(t, m.TargetRPS, "a search has no single target rate to record")

	target, ok := result.Target()
	require.True(t, ok)
	assert.Equal(t, "stubnode/v1.0.0", target.ClientVersion)
	assert.Equal(t, "1", target.ChainID)
}

func TestFindMaxRPSRejectsUnusableInput(t *testing.T) {
	url := cappedNode(t, 4, 10)

	t.Run("more than one target", func(t *testing.T) {
		cfg := searchConfig(url, 100)
		cfg.ResolvedClients = append(cfg.ResolvedClients, &types.ClientConfig{Name: "second", URL: url})

		_, err := FindMaxRPS(context.Background(), cfg, testOptions(t), fastSearch(DefaultSLO()))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "one target at a time")
	})

	t.Run("no latency ceiling", func(t *testing.T) {
		cfg := searchConfig(url, 100)
		_, err := FindMaxRPS(context.Background(), cfg, testOptions(t), fastSearch(SLO{ErrorRatePercent: 1, MinDeliveredPercent: 99}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "slo-p99")
	})
}

func TestEvaluateProbe(t *testing.T) {
	slo := SLO{P99Ms: 100, ErrorRatePercent: 1, MinDeliveredPercent: 99}

	client := func(requests int64, delivered float64, p99, errorRate float64) *types.ClientMetrics {
		scheduled := int64(float64(requests) / (delivered / 100))
		return &types.ClientMetrics{
			TotalRequests: requests,
			ErrorRate:     errorRate,
			Latency:       types.MetricSummary{P99: p99},
			Delivery:      types.DeliveryMetrics{Scheduled: scheduled, Sent: requests},
		}
	}

	cases := map[string]struct {
		client  *types.ClientMetrics
		verdict ProbeVerdict
		reason  string
	}{
		"within the slo":   {client: client(1000, 100, 50, 0), verdict: VerdictPass},
		"latency over":     {client: client(1000, 100, 500, 0), verdict: VerdictFail, reason: "p99"},
		"errors over":      {client: client(1000, 100, 50, 5), verdict: VerdictFail, reason: "error rate"},
		"not offered":      {client: client(600, 60, 50, 0), verdict: VerdictGeneratorLimited, reason: "vus"},
		"nothing sent":     {client: client(0, 100, 0, 0), verdict: VerdictGeneratorLimited},
		"no client at all": {client: nil, verdict: VerdictGeneratorLimited},
		// Delivery is judged before latency: a rate that was never offered says
		// nothing about the endpoint, however bad the latency looked.
		"not offered and slow": {client: client(600, 60, 5000, 0), verdict: VerdictGeneratorLimited, reason: "vus"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			probe := evaluateProbe(100, tc.client, slo)
			assert.Equal(t, tc.verdict, probe.Verdict)
			if tc.reason != "" {
				assert.Contains(t, probe.Reason, tc.reason)
			}
		})
	}
}

func TestFindMaxRPSHonoursCancellation(t *testing.T) {
	url := cappedNode(t, 4, 20)
	cfg := searchConfig(url, 500)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	search := fastSearch(DefaultSLO())
	search.ProbeDuration = 5 * time.Second

	start := time.Now()
	result, _ := FindMaxRPS(ctx, cfg, testOptions(t), search)
	assert.Less(t, time.Since(start), 8*time.Second)
	require.NotNil(t, result)
}
