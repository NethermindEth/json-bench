package metrics

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/types"
)

func summaryForAllPairs(cfg *config.Config) map[string]k6MetricValue {
	metrics := make(map[string]k6MetricValue)
	for _, client := range cfg.ResolvedClients {
		for _, call := range cfg.Calls {
			base := "{req_name:" + call.Name + ",scenario:" + client.Name + "}"
			metrics["http_req_duration"+base] = k6MetricValue{
				Avg: 10, Min: 1, Max: 100, Med: 8, P90: 20, P95: 30, P99: 90,
			}
			metrics["http_reqs"+base] = k6MetricValue{Count: 200, Rate: 20}
			metrics["http_req_failed"+base] = k6MetricValue{Rate: 0.01}
		}
	}
	// Top-level http_reqs is what runElapsedSeconds derives the run length from:
	// 800 requests at 40/s is a 20s run.
	metrics["http_reqs"] = k6MetricValue{Count: 800, Rate: 40}
	return metrics
}

func TestCollectClientsMetrics_NoPrometheus_UsesSummary(t *testing.T) {
	cfg := makeCfg()
	cfg.Outputs = &config.Outputs{}

	dir := t.TempDir()
	path := writeSummary(t, dir, summaryForAllPairs(cfg))

	logger, _ := makeLogger()
	got, err := CollectClientsMetrics(cfg, time.Time{}, path, logger)
	if err != nil {
		t.Fatalf("CollectClientsMetrics returned error: %v", err)
	}

	for _, client := range cfg.ResolvedClients {
		cm, ok := got[client.Name]
		if !ok {
			t.Fatalf("missing metrics for client %s", client.Name)
		}
		for _, call := range cfg.Calls {
			method, ok := cm.Methods[call.Name]
			if !ok {
				t.Fatalf("missing method %s for client %s", call.Name, client.Name)
			}
			if method.Count != 200 || method.Avg != 10 {
				t.Errorf("%s.%s not populated from summary: %+v", client.Name, call.Name, method)
			}
			// Throughput is k6's own rate, not the reciprocal of mean latency
			// (which would be 1000/10 = 100).
			if method.Throughput != 20 {
				t.Errorf("%s.%s throughput = %v, want k6's rate 20", client.Name, call.Name, method.Throughput)
			}
		}
		wantTotal := int64(200 * len(cfg.Calls))
		if cm.TotalRequests != wantTotal {
			t.Errorf("%s TotalRequests = %d, want %d", client.Name, cm.TotalRequests, wantTotal)
		}
		if cm.Latency.Avg <= 0 {
			t.Errorf("%s aggregate latency not finalized: %+v", client.Name, cm.Latency)
		}
		// 400 requests over the 20s run derived from the summary.
		if cm.Latency.Throughput != 20 {
			t.Errorf("%s aggregate throughput = %v, want 20", client.Name, cm.Latency.Throughput)
		}
	}
}

func TestCollectClientsMetrics_NilOutputs_UsesSummary(t *testing.T) {
	cfg := makeCfg()

	dir := t.TempDir()
	path := writeSummary(t, dir, summaryForAllPairs(cfg))

	logger, _ := makeLogger()
	got, err := CollectClientsMetrics(cfg, time.Time{}, path, logger)
	if err != nil {
		t.Fatalf("CollectClientsMetrics returned error: %v", err)
	}
	if len(got) != len(cfg.ResolvedClients) {
		t.Fatalf("expected %d clients, got %d", len(cfg.ResolvedClients), len(got))
	}
}

// A configured but unusable Prometheus must not empty the run: before this fix
// the query error propagated, the caller stored a nil map, and every CSV came
// out header-only while the run still reported success.
func TestCollectClientsMetrics_PrometheusUnreachable_FallsBackToSummary(t *testing.T) {
	// A listener that is closed immediately gives us an address nothing serves,
	// so the query fails the way an unused port does.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	deadURL := "http://" + listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	cfg := makeCfg()
	cfg.Outputs = &config.Outputs{PrometheusRW: &config.PrometheusRW{
		QueryURL: deadURL,
		Endpoint: deadURL + "/api/v1/write",
	}}

	path := writeSummary(t, t.TempDir(), summaryForAllPairs(cfg))

	logger, logs := makeLogger()
	got, err := CollectClientsMetrics(cfg, time.Time{}, path, logger)
	if err != nil {
		t.Fatalf("a failed Prometheus query must not fail collection: %v", err)
	}
	if got == nil {
		t.Fatal("CollectClientsMetrics must never return a nil map")
	}
	for _, client := range cfg.ResolvedClients {
		cm, ok := got[client.Name]
		if !ok {
			t.Fatalf("missing metrics for client %s", client.Name)
		}
		for _, call := range cfg.Calls {
			method, ok := cm.Methods[call.Name]
			if !ok || method.Count != 200 {
				t.Errorf("%s.%s should have been filled from summary.json, got %+v", client.Name, call.Name, method)
			}
		}
	}
	if !strings.Contains(logs.String(), "Prometheus was configured but could not be queried") {
		t.Errorf("expected a warning naming the fallback, got: %s", logs.String())
	}
}

// The Prometheus path has no rate to read, so throughput comes from
// count/elapsed rather than the reciprocal of mean latency.
func TestFinalizeClientMetrics_ThroughputFromElapsed(t *testing.T) {
	clients := map[string]*types.ClientMetrics{
		"geth": {
			Name: "geth",
			Methods: map[string]types.MetricSummary{
				"eth_call": {Count: 3600, Avg: 57.8},
			},
		},
	}

	finalizeClientMetrics(clients, 180)

	method := clients["geth"].Methods["eth_call"]
	if want := 20.0; method.Throughput != want {
		t.Errorf("method throughput = %v, want %v (3600 requests over 180s, not 1000/57.8)", method.Throughput, want)
	}
	if want := 20.0; clients["geth"].Latency.Throughput != want {
		t.Errorf("client throughput = %v, want %v", clients["geth"].Latency.Throughput, want)
	}
}

// With no way to know how long the run took, throughput stays zero so the CSV
// can report it as unmeasured instead of inventing a number.
func TestFinalizeClientMetrics_UnknownElapsedLeavesThroughputZero(t *testing.T) {
	clients := map[string]*types.ClientMetrics{
		"geth": {
			Name:    "geth",
			Methods: map[string]types.MetricSummary{"eth_call": {Count: 10, Avg: 5}},
		},
	}

	finalizeClientMetrics(clients, 0)

	if got := clients["geth"].Methods["eth_call"].Throughput; got != 0 {
		t.Errorf("throughput = %v, want 0 when the elapsed time is unknown", got)
	}
}
