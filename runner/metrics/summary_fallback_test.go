package metrics

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/types"
)

func makeLogger() (*logrus.Logger, *bytes.Buffer) {
	logger := logrus.New()
	buf := &bytes.Buffer{}
	logger.SetOutput(buf)
	logger.SetLevel(logrus.WarnLevel)
	logger.SetFormatter(&logrus.TextFormatter{DisableColors: true, DisableTimestamp: true})
	return logger, buf
}

func makeCfg() *config.Config {
	return &config.Config{
		ResolvedClients: []*types.ClientConfig{
			{Name: "geth", URL: "http://localhost:8545"},
			{Name: "nethermind", URL: "http://localhost:8546"},
		},
		Calls: []*config.Call{
			{Name: "eth_blockNumber", Method: "eth_blockNumber"},
			{Name: "eth_chainId", Method: "eth_chainId"},
		},
	}
}

func emptyClientMetrics(cfg *config.Config) map[string]*types.ClientMetrics {
	cm := make(map[string]*types.ClientMetrics)
	for _, c := range cfg.ResolvedClients {
		cm[c.Name] = &types.ClientMetrics{
			Name:    c.Name,
			Methods: make(map[string]types.MetricSummary),
		}
	}
	return cm
}

func writeSummary(t *testing.T, dir string, metrics map[string]k6MetricValue) string {
	t.Helper()
	path := filepath.Join(dir, "summary.json")
	body, err := json.Marshal(k6Summary{Metrics: metrics})
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestApplySummaryFallback_HappyPath(t *testing.T) {
	cfg := makeCfg()
	cm := emptyClientMetrics(cfg)
	for clientName := range cm {
		for _, call := range cfg.Calls {
			cm[clientName].Methods[call.Name] = types.MetricSummary{Count: 100, Avg: 5}
		}
	}
	before, err := json.Marshal(cm)
	if err != nil {
		t.Fatalf("marshal before: %v", err)
	}

	logger, buf := makeLogger()
	applySummaryFallback(cm, cfg, "/does/not/exist", logger)

	after, err := json.Marshal(cm)
	if err != nil {
		t.Fatalf("marshal after: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("ClientMetrics changed under happy path:\nbefore=%s\nafter=%s", before, after)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no warn lines, got: %q", buf.String())
	}
}

func TestApplySummaryFallback_FillsMissingPair(t *testing.T) {
	cfg := makeCfg()
	cm := emptyClientMetrics(cfg)
	cm["geth"].Methods["eth_chainId"] = types.MetricSummary{Count: 100, Avg: 5}
	cm["nethermind"].Methods["eth_blockNumber"] = types.MetricSummary{Count: 100, Avg: 5}
	cm["nethermind"].Methods["eth_chainId"] = types.MetricSummary{Count: 100, Avg: 5}

	dir := t.TempDir()
	path := writeSummary(t, dir, map[string]k6MetricValue{
		"http_req_duration{req_name:eth_blockNumber,scenario:geth}": {
			Avg: 12.5, Min: 1, Max: 100, Med: 8, P90: 25, P95: 35, P99: 80,
		},
		"http_reqs{req_name:eth_blockNumber,scenario:geth}":       {Count: 250},
		"http_req_failed{req_name:eth_blockNumber,scenario:geth}": {Rate: 0.02},
	})

	logger, buf := makeLogger()
	applySummaryFallback(cm, cfg, path, logger)

	got, ok := cm["geth"].Methods["eth_blockNumber"]
	if !ok {
		t.Fatalf("expected geth.eth_blockNumber to be populated, but it was missing")
	}
	if got.Count != 250 {
		t.Errorf("Count = %d, want 250", got.Count)
	}
	if got.Avg != 12.5 || got.P99 != 80 || got.Min != 1 || got.Max != 100 {
		t.Errorf("latency fields not populated: %+v", got)
	}
	if got.ErrorCount != 5 || got.SuccessCount != 245 {
		t.Errorf("counts not populated: errors=%d success=%d", got.ErrorCount, got.SuccessCount)
	}
	if got.ErrorRate <= 0 || got.SuccessRate <= 0 {
		t.Errorf("rates not derived: error=%.2f success=%.2f", got.ErrorRate, got.SuccessRate)
	}

	out := buf.String()
	if strings.Count(out, "Prometheus had no data for geth.eth_blockNumber") != 1 {
		t.Errorf("expected exactly one matching warn, got output:\n%s", out)
	}
	if strings.Count(out, "Prometheus had no data for") != 1 {
		t.Errorf("expected only one fallback warn total, got output:\n%s", out)
	}
}

func TestApplySummaryFallback_MissingSummaryFile(t *testing.T) {
	cfg := makeCfg()
	cm := emptyClientMetrics(cfg)
	cm["nethermind"].Methods["eth_blockNumber"] = types.MetricSummary{Count: 100, Avg: 5}
	cm["nethermind"].Methods["eth_chainId"] = types.MetricSummary{Count: 100, Avg: 5}
	cm["geth"].Methods["eth_chainId"] = types.MetricSummary{Count: 100, Avg: 5}

	logger, buf := makeLogger()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("applySummaryFallback panicked: %v", r)
		}
	}()
	applySummaryFallback(cm, cfg, "/tmp/no-such-summary-file.json", logger)

	if _, exists := cm["geth"].Methods["eth_blockNumber"]; exists {
		t.Errorf("expected eth_blockNumber to stay missing when summary cannot be read")
	}
	out := buf.String()
	if strings.Count(out, "Prometheus had no data for geth.eth_blockNumber") != 1 {
		t.Errorf("expected one fallback warn for the missing pair, got:\n%s", out)
	}
	if strings.Count(out, "Cannot read k6 summary") != 1 {
		t.Errorf("expected one warn about unreadable summary, got:\n%s", out)
	}
}
