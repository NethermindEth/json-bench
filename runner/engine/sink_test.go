package engine

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/engine/promrw"
	"github.com/jsonrpc-bench/runner/internal/promsink"
	"github.com/jsonrpc-bench/runner/internal/stubnode"
)

// runAgainstSink runs a short benchmark with remote write pointed at a
// recording sink, and returns what the sink received.
func runAgainstSink(t *testing.T, stubCfg stubnode.Config, mutate func(*config.Config)) *promsink.Recorder {
	t.Helper()

	stub, err := stubnode.New(stubCfg)
	require.NoError(t, err)
	node := httptest.NewServer(stub.Handler())
	t.Cleanup(node.Close)

	rec := promsink.NewRecorder()
	sinkServer := httptest.NewServer(rec.Handler())
	t.Cleanup(sinkServer.Close)

	client, err := promrw.New(promrw.Config{
		Endpoint: sinkServer.URL + promsink.WritePath,
		Username: "bench",
		Password: "s3cret",
	})
	require.NoError(t, err)

	cfg := runConfig(node.URL, []*config.Call{
		{Name: "eth_call", Method: "eth_call", Params: []any{}, Weight: 60},
		{Name: "eth_getLogs", Method: "eth_getLogs", Params: []any{}, Weight: 40},
	}, func(c *config.Config) {
		c.TestName = "series-golden"
		c.Duration = "2s"
		c.RPS = 60
		c.VUs = 10
		if mutate != nil {
			mutate(c)
		}
	})

	opts := testOptions(t)
	opts.Sink = NewPromSink(client)
	opts.PushInterval = 500 * time.Millisecond

	_, _, err = Run(context.Background(), cfg, opts)
	require.NoError(t, err)
	require.NoError(t, rec.Err())

	return rec
}

func TestPromSinkPublishesTheContract(t *testing.T) {
	rec := runAgainstSink(t, stubnode.Config{
		Seed:    31,
		Default: stubnode.Method{Latency: &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 2}},
	}, nil)

	assert.Greater(t, rec.Pushes(), 1, "the run must publish on the interval, not only at the end")

	headers := rec.Headers()
	assert.Equal(t, "snappy", headers["Content-Encoding"])
	assert.Equal(t, "application/x-protobuf", headers["Content-Type"])
	assert.Equal(t, "0.1.0", headers["X-Prometheus-Remote-Write-Version"])
	assert.Equal(t, "basic <redacted>", headers["Authorization"])
	assert.Contains(t, headers["User-Agent"], "jsonrpc-bench")
}

// Trends must be cumulative from the run's start, as k6's were: each dashboard
// latency panel aggregates the raw gauge, so a windowed value would silently
// change what every one of them means.
func TestPromSinkTrendsAreCumulative(t *testing.T) {
	rec := runAgainstSink(t, stubnode.Config{
		Seed:    37,
		Default: stubnode.Method{Latency: &stubnode.Latency{Kind: stubnode.LatencyUniform, MinMS: 1, MaxMS: 30}},
	}, nil)

	var checked int
	for _, series := range rec.Series() {
		if !strings.HasSuffix(series.Name, "_duration_max") || len(series.Values) < 3 {
			continue
		}
		checked++
		for i := 1; i < len(series.Values); i++ {
			assert.GreaterOrEqual(t, series.Values[i], series.Values[i-1],
				"%s must never decrease across pushes", series.Key())
		}
	}
	require.NotZero(t, checked, "no duration_max series carried enough pushes to check")

	// Counters accumulate in the ordinary Prometheus sense, so irate() panels work.
	for _, series := range rec.Series() {
		if !strings.HasSuffix(series.Name, "_iterations_total") || len(series.Values) < 3 {
			continue
		}
		for i := 1; i < len(series.Values); i++ {
			assert.GreaterOrEqual(t, series.Values[i], series.Values[i-1])
		}
		assert.Greater(t, series.Values[len(series.Values)-1], series.Values[0])
	}
}

// Remote write carries seconds; the CSV reports carry milliseconds.
func TestPromSinkPublishesSeconds(t *testing.T) {
	rec := runAgainstSink(t, stubnode.Config{
		Seed:    41,
		Default: stubnode.Method{Latency: &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 50}},
	}, nil)

	var found bool
	for _, series := range rec.Series() {
		if series.Name != "bench_http_req_duration_avg" {
			continue
		}
		found = true
		last := series.Values[len(series.Values)-1]
		assert.InDelta(t, 0.05, last, 0.02, "a 50ms call must publish as ~0.05, not 50")
	}
	assert.True(t, found, "bench_http_req_duration_avg was not published")
}

func TestPromSinkPublishesRPCErrorsAndSaturation(t *testing.T) {
	rec := runAgainstSink(t, stubnode.Config{
		Seed:    43,
		Default: stubnode.Method{Latency: &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 60}},
		Methods: map[string]stubnode.Method{
			"eth_call": {
				Latency:      &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 60},
				RPCErrorRate: 1, RPCErrorCode: -32000,
			},
		},
	}, func(c *config.Config) { c.VUs = 1 })

	byName := map[string][]promsink.Series{}
	for _, series := range rec.Series() {
		byName[series.Name] = append(byName[series.Name], series)
	}

	require.NotEmpty(t, byName["bench_rpc_errors_total"], "the node's error code must reach Prometheus")
	assert.Contains(t, byName["bench_rpc_errors_total"][0].Labels["rpc_code"], "-32000")

	require.NotEmpty(t, byName["bench_rpc_error_rate"])
	require.NotEmpty(t, byName["bench_requests_dropped_total"], "a saturated run must publish its shortfall")

	var dropped float64
	for _, series := range byName["bench_requests_dropped_total"] {
		dropped = series.Values[len(series.Values)-1]
	}
	assert.Greater(t, dropped, 0.0, "one VU at 60ms cannot offer 60 rps")

	// http_req_failed keeps k6's HTTP-only meaning, so the existing panel does
	// not silently change what it reports.
	require.NotEmpty(t, byName["bench_http_req_failed_rate"])
	for _, series := range byName["bench_http_req_failed_rate"] {
		if series.Labels["rpc_method"] == "eth_call" {
			assert.Zero(t, series.Values[len(series.Values)-1],
				"a JSON-RPC error is not an HTTP failure")
		}
	}
}

func TestPromSinkLabels(t *testing.T) {
	rec := runAgainstSink(t, stubnode.Config{
		Seed:    47,
		Default: stubnode.Method{Latency: &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 1}},
	}, nil)

	required := map[string][]string{
		"bench_http_req_duration_p99":  {"testid", "scenario", "client_type", "req_name", "rpc_method", "status"},
		"bench_http_reqs_total":        {"testid", "scenario", "client_type", "req_name", "rpc_method", "status", "outcome"},
		"bench_iterations_total":       {"testid", "scenario", "client_type"},
		"bench_requests_dropped_total": {"testid", "scenario", "client_type"},
	}

	found := map[string][]string{}
	for _, series := range rec.Series() {
		if _, ok := required[series.Name]; ok {
			found[series.Name] = series.LabelKeys
		}
		// The k6 labels that were dropped must not reappear.
		for _, dropped := range []string{"url", "group", "error", "error_code", "expected_response"} {
			assert.NotContains(t, series.LabelKeys, dropped,
				"%s carries the dropped k6 label %s", series.Name, dropped)
		}
	}

	for name, want := range required {
		got, ok := found[name]
		require.True(t, ok, "%s was not published", name)
		sort.Strings(want)
		assert.Equal(t, want, got, "label set for %s", name)
	}
}

// Every family the k6 output carried must map to a bench_* family or appear in
// the dropped list with a reason. This is what proves the migration is complete
// rather than accidentally lossy.
func TestMigrationCoversEveryK6Family(t *testing.T) {
	// Deliberate drops, with the reason each is safe to lose.
	dropped := map[string]string{
		"k6_group_duration": "k6 groups have no counterpart; no dashboard panel read this family",
		"k6_checks_rate":    "superseded by the outcome label, which carries strictly more and is read by the exports too",
		"k6_vus":            "renamed: bench_inflight counts requests in flight, which is the real quantity",
		"k6_vus_max":        "renamed: bench_inflight_max",
	}
	renamed := map[string]string{
		"k6_http_req_duration":        "bench_http_req_duration",
		"k6_http_req_blocked":         "bench_http_req_blocked",
		"k6_http_req_connecting":      "bench_http_req_connecting",
		"k6_http_req_tls_handshaking": "bench_http_req_tls_handshaking",
		"k6_http_req_sending":         "bench_http_req_sending",
		"k6_http_req_waiting":         "bench_http_req_waiting",
		"k6_http_req_receiving":       "bench_http_req_receiving",
		"k6_http_reqs_total":          "bench_http_reqs_total",
		"k6_http_req_failed_rate":     "bench_http_req_failed_rate",
		"k6_iteration_duration":       "bench_iteration_duration",
		"k6_iterations_total":         "bench_iterations_total",
		"k6_data_sent_total":          "bench_data_sent_total",
		"k6_data_received_total":      "bench_data_received_total",
		"k6_dropped_iterations_total": "bench_requests_dropped_total",
	}

	capture, err := os.ReadFile(filepath.Join("..", "internal", "promsink", "testdata", "k6-series.golden"))
	require.NoError(t, err)
	_, k6Keys, err := promsink.ParseGolden(capture)
	require.NoError(t, err)

	k6Names := map[string]bool{}
	for _, key := range k6Keys {
		name, _, _ := strings.Cut(key, "{")
		k6Names[trimTrendStat(name)] = true
	}
	require.NotEmpty(t, k6Names)

	emitted := emittedFamilies(t)

	for family := range k6Names {
		if reason, ok := dropped[family]; ok {
			assert.NotEmpty(t, reason)
			continue
		}
		target, ok := renamed[family]
		require.True(t, ok, "k6 family %s is neither mapped nor listed as dropped; the migration would lose it", family)
		assert.True(t, emitted[target], "%s maps to %s, which the engine does not emit", family, target)
	}
}

func TestEmittedFamiliesIncludeTheNewSignals(t *testing.T) {
	emitted := emittedFamilies(t)

	for _, family := range []string{
		"bench_rpc_error_rate",
		"bench_rpc_errors_total",
		"bench_requests_scheduled_total",
		"bench_requests_late_total",
		"bench_requests_dropped_total",
		"bench_queue_delay",
		"bench_achieved_rate",
		"bench_resp_bytes",
		"bench_inflight",
		"bench_inflight_max",
	} {
		assert.True(t, emitted[family], "%s is not emitted", family)
	}
}

// emittedFamilies renders a snapshot carrying every kind of observation and
// returns the metric families it produces.
func emittedFamilies(t *testing.T) map[string]bool {
	t.Helper()

	rec := runAgainstSink(t, stubnode.Config{
		Seed:    53,
		Default: stubnode.Method{Latency: &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 40}},
		Methods: map[string]stubnode.Method{
			"eth_call": {
				Latency:      &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 40},
				RPCErrorRate: 0.5, RPCErrorCode: -32000,
			},
		},
		Faults: stubnode.Faults{HTTP500Rate: 0.1},
	}, func(c *config.Config) { c.VUs = 1 })

	out := map[string]bool{}
	for _, name := range rec.MetricNames() {
		out[name] = true
		out[trimTrendStat(name)] = true
	}
	return out
}

func trimTrendStat(name string) string {
	for _, stat := range []string{"min", "max", "avg", "med", "p75", "p90", "p95", "p99", "p999"} {
		if base, ok := strings.CutSuffix(name, "_"+stat); ok {
			return base
		}
	}
	return name
}
