package engine

import (
	"context"
	"io"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/internal/stubnode"
	"github.com/jsonrpc-bench/runner/types"
)

func quietLogger() *logrus.Logger {
	log := logrus.New()
	log.SetOutput(io.Discard)
	log.SetLevel(logrus.ErrorLevel)
	return log
}

func stubServer(t *testing.T, cfg stubnode.Config) *httptest.Server {
	t.Helper()
	stub, err := stubnode.New(cfg)
	require.NoError(t, err)
	srv := httptest.NewServer(stub.Handler())
	t.Cleanup(srv.Close)
	return srv
}

// runConfig builds a benchmark config pointed at url, bypassing YAML so a test
// states only the load shape it cares about.
func runConfig(url string, calls []*config.Call, mutate func(*config.Config)) *config.Config {
	cfg := &config.Config{
		TestName:   "engine-test",
		ClientRefs: []string{"stub"},
		Duration:   "2s",
		RPS:        50,
		VUs:        10,
		Seed:       99,
		Calls:      calls,
		ResolvedClients: []*types.ClientConfig{
			{Name: "stub", Type: "nethermind", URL: url},
		},
	}
	if mutate != nil {
		mutate(cfg)
	}
	return cfg
}

func testOptions(t *testing.T) Options {
	opts := DefaultOptions()
	opts.OutputDir = t.TempDir()
	opts.Logger = quietLogger()
	return opts
}

// The k6 pipeline reported a 25%-error run as 0% errors, because a JSON-RPC
// error arrives as HTTP 200. This is the regression test for that.
func TestRunAttributesJSONRPCErrors(t *testing.T) {
	srv := stubServer(t, stubnode.Config{
		Seed:    5,
		Default: stubnode.Method{Latency: &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 1}},
		Methods: map[string]stubnode.Method{
			"eth_call": {RPCErrorRate: 1, RPCErrorCode: -32000},
		},
	})

	cfg := runConfig(srv.URL, []*config.Call{
		{Name: "eth_call", Method: "eth_call", Params: []any{}, Weight: 1},
	}, func(c *config.Config) { c.Duration = "1s"; c.RPS = 40 })

	result, breaches, err := Run(context.Background(), cfg, testOptions(t))
	require.NoError(t, err)
	require.Empty(t, breaches)

	client := result.ClientMetrics["stub"]
	require.NotNil(t, client)
	require.NotZero(t, client.TotalRequests)

	assert.Equal(t, client.TotalRequests, client.TotalErrors, "every response carried a JSON-RPC error")
	assert.InDelta(t, 100.0, client.ErrorRate, 0.001)
	assert.Equal(t, client.TotalRequests, client.ErrorTypes["rpc_error"])
	assert.Equal(t, client.TotalRequests, client.ErrorTypes["rpc_code_-32000"],
		"the node's own error code is preserved, not replaced by an opaque taxonomy")
	assert.Equal(t, client.TotalRequests, client.StatusCodes[200],
		"the requests were HTTP 200 throughout, which is why status alone cannot detect this")
}

func TestRunSeparatesOutcomeClasses(t *testing.T) {
	srv := stubServer(t, stubnode.Config{
		Seed:    11,
		Default: stubnode.Method{Latency: &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 1}},
		Methods: map[string]stubnode.Method{
			"eth_getLogs": {NullResultRate: 1},
		},
		Faults: stubnode.Faults{HTTP500Rate: 0.25},
	})

	cfg := runConfig(srv.URL, []*config.Call{
		{Name: "eth_getLogs", Method: "eth_getLogs", Params: []any{}, Weight: 1},
	}, func(c *config.Config) { c.Duration = "1s"; c.RPS = 60 })

	result, _, err := Run(context.Background(), cfg, testOptions(t))
	require.NoError(t, err)

	client := result.ClientMetrics["stub"]
	require.NotZero(t, client.TotalRequests)

	assert.NotZero(t, client.ErrorTypes["http_error"], "the injected 500s must be counted")
	assert.NotZero(t, client.StatusCodes[500])
	assert.Less(t, client.ErrorRate, 100.0, "the null results are successful calls, not errors")

	// A null result is a success that returned nothing, so it must not inflate
	// the error rate but must remain visible.
	summary := client.Methods["eth_getLogs"]
	assert.Greater(t, summary.SuccessCount, int64(0))
	assert.Equal(t, summary.Count, summary.SuccessCount+summary.ErrorCount)
}

// A run whose in-flight limit cannot keep up used to be indistinguishable from
// a healthy one. Under the drop policy the shortfall is counted; under queue it
// is measured as dispatch delay.
func TestRunReportsUnderDeliveredLoad(t *testing.T) {
	srv := stubServer(t, stubnode.Config{
		Seed:    3,
		Default: stubnode.Method{Latency: &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 60}},
	})

	calls := []*config.Call{{Name: "eth_call", Method: "eth_call", Params: []any{}, Weight: 1}}

	t.Run("drop counts what was never sent", func(t *testing.T) {
		cfg := runConfig(srv.URL, calls, func(c *config.Config) {
			c.Duration = "1s"
			c.RPS = 100
			c.VUs = 1
		})
		opts := testOptions(t)
		opts.Saturation = SaturationDrop

		result, _, err := Run(context.Background(), cfg, opts)
		require.NoError(t, err)

		delivery := deliveryFromSummary(t, result, "stub")
		assert.Greater(t, delivery["dropped"], 0.0, "one VU at 60ms cannot offer 100 rps")
		assert.Less(t, delivery["sent"], delivery["scheduled"])
		assert.Less(t, delivery["achieved_rps"], 100.0)
	})

	t.Run("queue records how late requests went out", func(t *testing.T) {
		cfg := runConfig(srv.URL, calls, func(c *config.Config) {
			c.Duration = "1s"
			c.RPS = 100
			c.VUs = 1
		})
		opts := testOptions(t)
		opts.Saturation = SaturationQueue

		result, _, err := Run(context.Background(), cfg, opts)
		require.NoError(t, err)

		delivery := deliveryFromSummary(t, result, "stub")
		assert.Greater(t, delivery["late"], 0.0)
		assert.Greater(t, delivery["max_queue_delay_ms"], 0.0,
			"the dispatch delay is the coordinated-omission error a saturated run otherwise hides")
	})

	t.Run("abort refuses to report numbers it cannot stand behind", func(t *testing.T) {
		cfg := runConfig(srv.URL, calls, func(c *config.Config) {
			c.Duration = "1s"
			c.RPS = 100
			c.VUs = 1
		})
		opts := testOptions(t)
		opts.Saturation = SaturationAbort

		_, _, err := Run(context.Background(), cfg, opts)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrSaturated)
	})
}

func TestRunDeliversTheRequestedLoadWhenItCan(t *testing.T) {
	srv := stubServer(t, stubnode.Config{
		Seed:    7,
		Default: stubnode.Method{Latency: &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 2}},
	})

	cfg := runConfig(srv.URL, []*config.Call{
		{Name: "eth_blockNumber", Method: "eth_blockNumber", Params: []any{}, Weight: 1},
	}, func(c *config.Config) { c.Duration = "2s"; c.RPS = 50; c.VUs = 10 })

	result, _, err := Run(context.Background(), cfg, testOptions(t))
	require.NoError(t, err)

	delivery := deliveryFromSummary(t, result, "stub")
	assert.EqualValues(t, 100, delivery["scheduled"], "50 rps for 2s")
	assert.EqualValues(t, 100, delivery["sent"])
	assert.Zero(t, delivery["dropped"])
	assert.InDelta(t, 50.0, delivery["achieved_rps"], 5.0)
}

// The client-level percentile must come from that client's pooled samples. The
// k6 pipeline averaged the per-method percentiles, which is not a percentile of
// any distribution: two methods at 8ms and 58ms reported a client p95 of 33ms,
// a latency no request ever had.
func TestClientLatencyIsARealPercentile(t *testing.T) {
	srv := stubServer(t, stubnode.Config{
		Seed:    13,
		Default: stubnode.Method{Latency: &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 5}},
		Methods: map[string]stubnode.Method{
			"eth_getLogs": {Latency: &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 60}},
		},
	})

	cfg := runConfig(srv.URL, []*config.Call{
		{Name: "eth_call", Method: "eth_call", Params: []any{}, Weight: 90},
		{Name: "eth_getLogs", Method: "eth_getLogs", Params: []any{}, Weight: 10},
	}, func(c *config.Config) { c.Duration = "2s"; c.RPS = 60; c.VUs = 20 })

	result, _, err := Run(context.Background(), cfg, testOptions(t))
	require.NoError(t, err)

	client := result.ClientMetrics["stub"]
	fast := client.Methods["eth_call"].P95
	slow := client.Methods["eth_getLogs"].P95
	overall := client.Latency.P95

	require.Greater(t, slow, fast)
	meanOfPercentiles := (fast + slow) / 2
	assert.Greater(t, math.Abs(overall-meanOfPercentiles), 5.0,
		"a 90/10 mix must not report the midpoint of its two methods' p95s (%.1f)", meanOfPercentiles)

	// With 90% of requests near 5ms, the pooled p95 sits in the slow tail
	// rather than halfway between the two methods.
	assert.GreaterOrEqual(t, overall, fast)
	assert.LessOrEqual(t, overall, slow)
}

func TestRunFillsStatisticsThatUsedToBeUnavailable(t *testing.T) {
	srv := stubServer(t, stubnode.Config{
		Seed:    17,
		Default: stubnode.Method{Latency: &stubnode.Latency{Kind: stubnode.LatencyUniform, MinMS: 2, MaxMS: 40}},
	})

	cfg := runConfig(srv.URL, []*config.Call{
		{Name: "eth_call", Method: "eth_call", Params: []any{}, Weight: 1},
	}, func(c *config.Config) { c.Duration = "2s"; c.RPS = 80; c.VUs = 20 })

	result, _, err := Run(context.Background(), cfg, testOptions(t))
	require.NoError(t, err)

	summary := result.ClientMetrics["stub"].Methods["eth_call"]
	require.NotZero(t, summary.Count)

	assert.NotZero(t, summary.Variance, "computable from samples, previously always NA")
	assert.NotZero(t, summary.IQR)
	assert.NotZero(t, summary.MAD)
	assert.NotZero(t, summary.P999)

	// Std dev used to be (max-min)/4. For a uniform draw the true value is
	// range/sqrt(12) ~= range/3.46, so the stand-in was ~14% low; more
	// importantly it was a function of two order statistics rather than of the
	// distribution.
	assert.Greater(t, math.Abs(summary.StdDev-(summary.Max-summary.Min)/4), 0.5,
		"std dev must come from the samples, not from the range")
	assert.Greater(t, summary.StdDev, 0.0)
	assert.Less(t, summary.StdDev, summary.Max-summary.Min)
}

func TestRunWritesReplayableSamples(t *testing.T) {
	srv := stubServer(t, stubnode.Config{
		Seed:    19,
		Default: stubnode.Method{Latency: &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 1}},
		Methods: map[string]stubnode.Method{"eth_call": {RPCErrorRate: 0.5}},
	})

	cfg := runConfig(srv.URL, []*config.Call{
		{Name: "eth_call", Method: "eth_call", Params: []any{}, Weight: 1},
	}, func(c *config.Config) { c.Duration = "1s"; c.RPS = 40 })

	opts := testOptions(t)
	result, _, err := Run(context.Background(), cfg, opts)
	require.NoError(t, err)

	file, err := os.Open(filepath.Join(opts.OutputDir, SampleFilename))
	require.NoError(t, err)
	defer file.Close()

	samples, err := ReadSamples(file)
	require.NoError(t, err)

	assert.EqualValues(t, result.ClientMetrics["stub"].TotalRequests, len(samples))

	// Re-deriving a percentile from the file must agree with the report, which
	// is what makes offline re-aggregation trustworthy.
	g := newGroup()
	for _, s := range samples {
		g.add(s)
	}
	recomputed := g.summarize(0)
	assert.InDelta(t, result.ClientMetrics["stub"].Latency.P99, recomputed.P99, 0.001)
	assert.Equal(t, result.ClientMetrics["stub"].TotalErrors, recomputed.ErrorCount)
}

func TestRunAppliesThresholds(t *testing.T) {
	srv := stubServer(t, stubnode.Config{
		Seed:    23,
		Default: stubnode.Method{Latency: &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 40}},
	})

	newCfg := func(threshold string) *config.Config {
		return runConfig(srv.URL, []*config.Call{
			{Name: "eth_call", Method: "eth_call", Params: []any{}, Weight: 1, Thresholds: []string{threshold}},
		}, func(c *config.Config) { c.Duration = "1s"; c.RPS = 30; c.VUs = 10 })
	}

	t.Run("breach is reported", func(t *testing.T) {
		_, breaches, err := Run(context.Background(), newCfg("p(99)<5"), testOptions(t))
		require.NoError(t, err)
		require.Len(t, breaches, 1)
		assert.Equal(t, "eth_call", breaches[0].Target)
		assert.Greater(t, breaches[0].Actual, 5.0)
	})

	t.Run("satisfied threshold passes", func(t *testing.T) {
		_, breaches, err := Run(context.Background(), newCfg("p(99)<5000"), testOptions(t))
		require.NoError(t, err)
		assert.Empty(t, breaches)
	})
}

func TestRunHonoursCancellation(t *testing.T) {
	srv := stubServer(t, stubnode.Config{
		Seed:    29,
		Default: stubnode.Method{Latency: &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 5}},
	})

	cfg := runConfig(srv.URL, []*config.Call{
		{Name: "eth_call", Method: "eth_call", Params: []any{}, Weight: 1},
	}, func(c *config.Config) { c.Duration = "30s"; c.RPS = 20; c.VUs = 5 })

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	start := time.Now()
	result, _, _ := Run(ctx, cfg, testOptions(t))
	require.NotNil(t, result)
	assert.Less(t, time.Since(start), 5*time.Second, "cancellation must not wait out the configured duration")
}

func deliveryFromSummary(t *testing.T, result *types.BenchmarkResult, client string) map[string]float64 {
	t.Helper()
	delivery, ok := result.Summary["delivery"].(map[string]any)
	require.True(t, ok, "the summary must carry the delivery accounting")
	entry, ok := delivery[client].(map[string]any)
	require.True(t, ok, "no delivery entry for %s", client)

	out := make(map[string]float64, len(entry))
	for key, value := range entry {
		switch v := value.(type) {
		case int:
			out[key] = float64(v)
		case float64:
			out[key] = v
		}
	}
	return out
}
