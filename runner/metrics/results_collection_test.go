package metrics

import (
	"time"

	"testing"

	"github.com/jsonrpc-bench/runner/config"
)

func summaryForAllPairs(cfg *config.Config) map[string]k6MetricValue {
	metrics := make(map[string]k6MetricValue)
	for _, client := range cfg.ResolvedClients {
		for _, call := range cfg.Calls {
			base := "{req_name:" + call.Name + ",scenario:" + client.Name + "}"
			metrics["http_req_duration"+base] = k6MetricValue{
				Avg: 10, Min: 1, Max: 100, Med: 8, P90: 20, P95: 30, P99: 90,
			}
			metrics["http_reqs"+base] = k6MetricValue{Count: 200}
			metrics["http_req_failed"+base] = k6MetricValue{Rate: 0.01}
		}
	}
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
		}
		wantTotal := int64(200 * len(cfg.Calls))
		if cm.TotalRequests != wantTotal {
			t.Errorf("%s TotalRequests = %d, want %d", client.Name, cm.TotalRequests, wantTotal)
		}
		if cm.Latency.Avg <= 0 {
			t.Errorf("%s aggregate latency not finalized: %+v", client.Name, cm.Latency)
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
