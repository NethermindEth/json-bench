package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/engine/promrw"
	"github.com/jsonrpc-bench/runner/internal/stubnode"
	"github.com/jsonrpc-bench/runner/types"
)

// The parser's zero value leaves its validation scheme unset and panics on the
// first metric name, so it has to be built with a scheme. A node's endpoint is
// arbitrary text from another process, so a parse failure must also never take
// the run down.
func TestParseExposition(t *testing.T) {
	t.Run("reads counters, gauges and labels", func(t *testing.T) {
		families, err := parseExposition(strings.NewReader(
			"# TYPE process_cpu_seconds_total counter\n" +
				"process_cpu_seconds_total 12.5\n" +
				"# TYPE nethermind_blocks gauge\n" +
				"nethermind_blocks{chain=\"gnosis\"} 38000000\n"))
		require.NoError(t, err)
		require.Len(t, families, 2)

		assert.Equal(t, "counter", kindName(families["process_cpu_seconds_total"].GetType()))
		assert.Equal(t, "gauge", kindName(families["nethermind_blocks"].GetType()))
		assert.Equal(t, "gnosis", labelsOf(families["nethermind_blocks"].GetMetric()[0])["chain"])
	})

	t.Run("a UTF-8 name is accepted rather than rejected", func(t *testing.T) {
		_, err := parseExposition(strings.NewReader("# TYPE a_metric gauge\na_metric{lab=\"café\"} 1\n"))
		assert.NoError(t, err)
	})

	t.Run("malformed input is an error, not a crash", func(t *testing.T) {
		for _, body := range []string{
			"this is not the exposition format\n",
			"# TYPE x counter\nx not_a_number\n",
			"\x00\x01\x02",
			"# TYPE x counter\nx{unclosed=\"1 2\n",
		} {
			_, err := parseExposition(strings.NewReader(body))
			assert.Error(t, err, "%q", body)
		}
	})
}

func TestTargetMetricPatterns(t *testing.T) {
	opts := TargetMetricsOptions{Patterns: []string{"process_cpu_seconds_total", "nethermind_*"}}

	assert.True(t, opts.matches("process_cpu_seconds_total"))
	assert.True(t, opts.matches("nethermind_blocks"))
	assert.True(t, opts.matches("nethermind_"))
	assert.False(t, opts.matches("process_cpu_seconds_total_extra"), "an exact pattern must not match by prefix")
	assert.False(t, opts.matches("go_goroutines"))

	assert.False(t, TargetMetricsOptions{}.matches("anything"), "no patterns selects nothing")
}

// metricsEndpoint serves a body that changes between reads, so a counter has a
// delta to find.
func metricsEndpoint(t *testing.T) (string, *atomic.Int64) {
	t.Helper()
	var counter atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := counter.Load()
		fmt.Fprintf(w, "# TYPE work_total counter\nwork_total %d\n"+
			"# TYPE memory_bytes gauge\nmemory_bytes %d\n"+
			"# TYPE gc_total counter\ngc_total{generation=\"0\"} %d\n", n, 1000+n, n/2)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &counter
}

// A counter's absolute value counts from when the node started, so a report
// needs its change over the run; a gauge needs its range.
func TestScraperSummarizesCountersAsDeltasAndGaugesAsRanges(t *testing.T) {
	url, counter := metricsEndpoint(t)
	scraper := NewTargetScraper("node", url, TargetMetricsOptions{
		Interval: 20 * time.Millisecond,
		Patterns: []string{"work_total", "memory_bytes", "gc_total"},
	}, quietLogger())

	counter.Store(100)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); scraper.Run(ctx) }()

	time.Sleep(60 * time.Millisecond)
	counter.Store(180)
	time.Sleep(60 * time.Millisecond)
	cancel()
	<-done

	require.NotEmpty(t, scraper.Summarize())
	for _, m := range scraper.Summarize() {
		switch m.Label() {
		case "work_total":
			assert.Equal(t, "counter", m.Kind)
			assert.EqualValues(t, 80, m.Delta, "180 - 100")
			assert.Greater(t, m.PerSecond, 0.0)
		case "memory_bytes":
			assert.Equal(t, "gauge", m.Kind)
			assert.EqualValues(t, 1100, m.Min)
			assert.EqualValues(t, 1180, m.Max)
			assert.Zero(t, m.Delta, "a gauge has no delta to report")
		case "gc_total{generation=0}":
			assert.Equal(t, "counter", m.Kind)
			assert.EqualValues(t, 40, m.Delta, "90 - 50, kept per label")
		default:
			t.Errorf("unexpected series %q", m.Label())
		}
	}

	errs, err := scraper.Err()
	assert.Zero(t, errs)
	assert.NoError(t, err)
}

// A gap in the node-side figures has to be distinguishable from the node having
// been idle.
func TestScraperRecordsFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "metrics disabled", http.StatusNotFound)
	}))
	defer srv.Close()

	scraper := NewTargetScraper("node", srv.URL, TargetMetricsOptions{
		Interval: 20 * time.Millisecond,
		Patterns: []string{"anything"},
	}, quietLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	scraper.Run(ctx)

	errs, err := scraper.Err()
	assert.NotZero(t, errs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
	assert.Empty(t, scraper.Summarize())
}

func TestScraperIsSkippedWithoutAMetricsURL(t *testing.T) {
	srv := stubServer(t, stubnode.Config{
		Default: stubnode.Method{Latency: &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 1}},
	})

	cfg := runConfig(srv.URL, []*config.Call{
		{Name: "eth_call", Method: "eth_call", Params: []any{}, Weight: 1},
	}, func(c *config.Config) { c.Duration = "1s"; c.RPS = 20; c.VUs = 5 })

	result, _, err := Run(context.Background(), cfg, testOptions(t))
	require.NoError(t, err)

	assert.Nil(t, result.ClientMetrics["stub"].TargetMetrics,
		"a client with no metrics_url reports nothing, which is not the same as reporting zero")
}

// The point of reading the node's own metrics: its figures and the client's
// have to agree about the same run.
func TestRunCorrelatesTheNodesOwnMetrics(t *testing.T) {
	stubCfg := stubnode.DefaultConfig()
	stubCfg.Node.Metrics = true
	stubCfg.Node.CPUSecondsPerRequest = 0.002
	stubCfg.Default.Latency = &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 1}
	srv := stubServer(t, stubCfg)

	cfg := runConfig(srv.URL, []*config.Call{
		{Name: "eth_call", Method: "eth_call", Params: []any{}, Weight: 1},
	}, func(c *config.Config) { c.Duration = "2s"; c.RPS = 50; c.VUs = 10 })
	cfg.ResolvedClients[0].MetricsURL = srv.URL + "/metrics"

	opts := testOptions(t)
	opts.TargetMetrics = TargetMetricsOptions{
		Interval: 200 * time.Millisecond,
		Patterns: []string{"process_*", "stub_*"},
	}

	result, _, err := Run(context.Background(), cfg, opts)
	require.NoError(t, err)

	client := result.ClientMetrics["stub"]
	require.NotNil(t, client.TargetMetrics)
	assert.Zero(t, client.TargetMetrics.ScrapeErrors)
	assert.Equal(t, srv.URL+"/metrics", client.TargetMetrics.Endpoint)

	byName := map[string]float64{}
	kinds := map[string]string{}
	for _, m := range client.TargetMetrics.Metrics {
		byName[m.Label()] = m.Delta
		kinds[m.Label()] = m.Kind
		if m.Kind == "gauge" {
			byName[m.Label()] = m.Max
		}
	}

	// The node counted the same requests the client sent.
	assert.InDelta(t, float64(client.TotalRequests), byName["stub_requests_total"], 2,
		"the node's own request counter must agree with what the client sent")
	assert.Equal(t, "counter", kinds["process_cpu_seconds_total"])
	assert.Greater(t, byName["process_cpu_seconds_total"], 0.0)
	assert.Greater(t, byName["process_resident_memory_bytes"], 0.0)
}

// Republished node metrics must not be able to overwrite which client a sample
// came from.
func TestTargetSeriesCannotOverwriteTheRunsIdentityLabels(t *testing.T) {
	snap := fullSnapshot()
	snap.Clients[0].Target = []types.TargetMetricPoint{{
		Name: "sneaky",
		Kind: "gauge",
		// A node is free to publish a label called scenario.
		Labels: map[string]string{"scenario": "someone_elses_node", "instance": "a"},
		Value:  1,
	}}

	var found bool
	for _, series := range BuildSeries(snap) {
		if series.Name != "bench_target_sneaky" {
			continue
		}
		found = true
		assert.Equal(t, "nethermind", series.Labels["scenario"], "the run's own client name must win")
		assert.Equal(t, "a", series.Labels["instance"], "the node's other labels are kept")
	}
	assert.True(t, found, "bench_target_sneaky was not emitted")
}

// The republished families cannot be enumerated from a capture, since their
// names come from the node. This pins the prefix and the labels instead, which
// is what a dashboard query binds to.
func TestRepublishedTargetSeriesShape(t *testing.T) {
	snap := fullSnapshot()
	snap.Clients[0].Target = []types.TargetMetricPoint{
		{Name: "process_cpu_seconds_total", Kind: "counter", Value: 12.5},
		{Name: "dotnet_collection_count_total", Kind: "counter", Labels: map[string]string{"generation": "0"}, Value: 40},
	}

	byName := map[string]promrw.Series{}
	for _, series := range BuildSeries(snap) {
		byName[series.Name] = series
	}

	cpu, ok := byName["bench_target_process_cpu_seconds_total"]
	require.True(t, ok, "the node's family must be republished under the bench_target_ prefix")
	assert.InDelta(t, 12.5, cpu.Value, 1e-9, "a node counter is republished as-is, not converted")
	assert.Equal(t, "nethermind", cpu.Labels["scenario"])
	assert.Equal(t, "golden", cpu.Labels["testid"])

	gc, ok := byName["bench_target_dotnet_collection_count_total"]
	require.True(t, ok)
	assert.Equal(t, "0", gc.Labels["generation"], "the node's own labels are kept")
	assert.Equal(t, "nethermind", gc.Labels["scenario"])
}
