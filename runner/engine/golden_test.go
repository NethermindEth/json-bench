package engine

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/internal/promsink"
	"github.com/jsonrpc-bench/runner/types"
)

const benchGoldenPath = "testdata/bench-series.golden"

// fullSnapshot carries one of every kind of observation, so rendering it
// produces the complete series set rather than whichever subset a particular
// run happened to exercise.
func fullSnapshot() Snapshot {
	summary := types.MetricSummary{
		Count: 10, Min: 1, Max: 90, Avg: 20,
		P50: 15, P75: 25, P90: 40, P95: 60, P99: 85, P999: 89,
	}
	phases := make(map[Phase]types.MetricSummary, len(PhaseNames))
	for _, phase := range PhaseNames {
		phases[phase] = summary
	}

	return Snapshot{
		TestName: "golden",
		At:       time.Unix(1730000000, 0),
		Clients: []ClientSeries{{
			Name:       "nethermind",
			ClientType: "nethermind",
			Duration:   summary,
			QueueDelay: summary,
			Count:      10,
			Errors:     3,
			ReqBytes:   2048,
			RespBytes:  8192,
			Delivery: Delivery{
				Scheduled: 12, Sent: 10, Late: 2, Dropped: 2,
				Inflight: 1, InflightPeak: 5,
				Started: time.Unix(1730000000, 0), Finished: time.Unix(1730000010, 0),
			},
			Series: []MethodSeries{
				{Name: "eth_call", Method: "eth_call", Status: 200, Outcome: OutcomeOK,
					Phases: phases, RespBytes: summary, Count: 7},
				{Name: "eth_call", Method: "eth_call", Status: 200, Outcome: OutcomeRPCError,
					Phases: phases, RespBytes: summary, Count: 2},
				{Name: "eth_call", Method: "eth_call", Status: 500, Outcome: OutcomeHTTPError,
					Phases: phases, RespBytes: summary, Count: 1},
			},
			Totals: []MethodTotals{{
				Name: "eth_call", Method: "eth_call",
				Count: 10, Errors: 3, HTTPFails: 1, RPCErrors: 2,
				RPCCodes: map[int]int64{-32000: 2},
			}},
		}},
	}
}

func renderedKeys(t *testing.T) []string {
	t.Helper()

	seen := make(map[string]struct{})
	for _, series := range BuildSeries(fullSnapshot()) {
		keys := make([]string, 0, len(series.Labels))
		for name := range series.Labels {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		seen[series.Name+"{"+strings.Join(keys, ",")+"}"] = struct{}{}
	}

	out := make([]string, 0, len(seen))
	for key := range seen {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// The committed capture is what the dashboards and metrics/METRICS.md are
// written against. A renamed series, a dropped family or an added label has to
// fail here, because nothing downstream would report it: a Grafana panel with a
// stale name simply draws nothing.
//
// Regenerate with:
//
//	go run ./runner/cmd/stubnode -config stub.json &
//	go run ./runner/cmd/promsink -listen 127.0.0.1:19091 \
//	    -source "jsonrpc-bench native engine" -golden runner/engine/testdata/bench-series.golden &
//	go run ./runner benchmark --config bench.yaml --clients clients.yaml \
//	    --prometheus http://127.0.0.1:19091 --prometheus-rw-user u --prometheus-rw-pass p
func TestEmittedSeriesMatchTheGolden(t *testing.T) {
	data, err := os.ReadFile(benchGoldenPath)
	require.NoError(t, err)

	headers, keys, err := promsink.ParseGolden(data)
	require.NoError(t, err)

	assert.Equal(t, "snappy", headers["Content-Encoding"])
	assert.Equal(t, "application/x-protobuf", headers["Content-Type"])
	assert.Equal(t, "0.1.0", headers["X-Prometheus-Remote-Write-Version"])
	assert.Contains(t, headers["User-Agent"], "jsonrpc-bench")
	assert.Equal(t, "basic <redacted>", headers["Authorization"],
		"remote-write auth must be exercised by the capture but never written to it")

	assert.Equal(t, keys, renderedKeys(t))
}

// Unlike k6, whose error and error_code labels appeared only on failing series,
// every family here has one label-key set for the life of the run. A node that
// starts failing therefore adds samples to existing series instead of minting
// new ones.
func TestSeriesLabelKeysDoNotDependOnOutcomes(t *testing.T) {
	healthy := fullSnapshot()
	healthy.Clients[0].Series = healthy.Clients[0].Series[:1]
	healthy.Clients[0].Totals[0].RPCCodes = nil
	healthy.Clients[0].Totals[0].RPCErrors = 0
	healthy.Clients[0].Totals[0].HTTPFails = 0

	keysOf := func(snap Snapshot) map[string][]string {
		out := map[string][]string{}
		for _, series := range BuildSeries(snap) {
			keys := make([]string, 0, len(series.Labels))
			for name := range series.Labels {
				keys = append(keys, name)
			}
			sort.Strings(keys)
			if existing, ok := out[series.Name]; ok {
				assert.Equal(t, existing, keys, "%s has more than one label-key set", series.Name)
				continue
			}
			out[series.Name] = keys
		}
		return out
	}

	failing := keysOf(fullSnapshot())
	fine := keysOf(healthy)

	for name, keys := range fine {
		if name == Namespace+"_rpc_errors_total" {
			continue
		}
		assert.Equal(t, failing[name], keys, "label keys for %s changed with the outcome mix", name)
	}

	// The per-code family is the one exception, and only because it cannot exist
	// without a code to label it.
	assert.NotContains(t, fine, Namespace+"_rpc_errors_total")
}

func TestTrendFamiliesCarryTheWholeStatLadder(t *testing.T) {
	rendered := renderedKeys(t)

	names := make(map[string]bool, len(rendered))
	for _, key := range rendered {
		name, _, _ := strings.Cut(key, "{")
		names[name] = true
	}

	// The dashboard populates its stat picker by discovering the emitted names,
	// so a partial ladder silently shortens the menu.
	for _, family := range []string{
		"bench_http_req_duration",
		"bench_http_req_waiting",
		"bench_http_req_sending",
		"bench_http_req_receiving",
		"bench_http_req_blocked",
		"bench_http_req_connecting",
		"bench_http_req_tls_handshaking",
		"bench_iteration_duration",
		"bench_queue_delay",
	} {
		for _, stat := range []string{"min", "max", "avg", "med", "p75", "p90", "p95", "p99", "p999"} {
			assert.True(t, names[family+"_"+stat], "%s_%s is not emitted", family, stat)
		}
	}
}

func TestDurationsArePublishedInSeconds(t *testing.T) {
	for _, series := range BuildSeries(fullSnapshot()) {
		if series.Name != "bench_http_req_duration_p99" {
			continue
		}
		// The snapshot carries 85ms, and Prometheus base units are seconds.
		assert.InDelta(t, 0.085, series.Value, 1e-9)
		return
	}
	t.Fatal("bench_http_req_duration_p99 was not emitted")
}

func TestGoldenFileIsSorted(t *testing.T) {
	data, err := os.ReadFile(filepath.Clean(benchGoldenPath))
	require.NoError(t, err)
	_, keys, err := promsink.ParseGolden(data)
	require.NoError(t, err)

	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	assert.Equal(t, sorted, keys, "series must stay sorted so the file diffs cleanly")
}
