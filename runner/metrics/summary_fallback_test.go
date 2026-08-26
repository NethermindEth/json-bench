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

// The summary path must enumerate and look up the same keys the k6 thresholds
// were registered under: for a calls-file run that is the rpc_method tag over
// the file's methods, not the declared call name.
func TestApplySummaryFallback_CallsFileKeysOnRPCMethod(t *testing.T) {
	cfg := makeCfg()
	cfg.Calls = []*config.Call{{Name: "multimethod", Method: "eth_call"}}
	cfg.CallsFile = "requests.csv"
	cfg.CallsFileMethods = []string{"eth_call", "eth_getLogs"}

	metrics := make(map[string]k6MetricValue)
	for _, client := range cfg.ResolvedClients {
		for _, method := range cfg.CallsFileMethods {
			base := "{scenario:" + client.Name + ",rpc_method:" + method + "}"
			metrics["http_req_duration"+base] = k6MetricValue{Avg: 12, Min: 2, Max: 40, Med: 9, P90: 20, P95: 25, P99: 35}
			metrics["http_reqs"+base] = k6MetricValue{Count: 150, Rate: 15}
		}
	}
	// The declared name carries the zero-count submetric the old code found.
	for _, client := range cfg.ResolvedClients {
		metrics["http_req_duration{scenario:"+client.Name+",req_name:multimethod}"] = k6MetricValue{}
	}

	path := writeSummary(t, t.TempDir(), metrics)
	clientsMetrics := emptyClientMetrics(cfg)
	logger, _ := makeLogger()
	applySummaryFallback(clientsMetrics, cfg, path, logger)

	for _, client := range cfg.ResolvedClients {
		cm := clientsMetrics[client.Name]
		if _, ok := cm.Methods["multimethod"]; ok {
			t.Errorf("%s: the declared call name should not be a method row for a calls-file run", client.Name)
		}
		for _, method := range cfg.CallsFileMethods {
			got, ok := cm.Methods[method]
			if !ok {
				t.Fatalf("%s: missing method %s", client.Name, method)
			}
			if got.Count != 150 || got.Throughput != 15 {
				t.Errorf("%s.%s = %+v, want count 150 and throughput 15", client.Name, method, got)
			}
		}
	}
}

func TestExtractMethodFromSummary_ReadsP75AndP999(t *testing.T) {
	metrics := map[string]k6MetricValue{
		"http_req_duration{scenario:geth,req_name:eth_call}": {
			Avg: 10, Min: 1, Max: 100, Med: 8, P75: 12, P90: 20, P95: 30, P99: 90, P999: 99,
		},
		"http_reqs{scenario:geth,req_name:eth_call}": {Count: 10, Rate: 5},
	}
	summary := &k6Summary{Metrics: metrics}

	method := extractMethodFromSummary(summary, "req_name", "geth", "eth_call")
	if method == nil {
		t.Fatal("expected a method summary")
	}
	if method.P75 != 12 || method.P999 != 99 {
		t.Errorf("P75/P99.9 = %v/%v, want 12/99", method.P75, method.P999)
	}
	if method.Throughput != 5 {
		t.Errorf("throughput = %v, want k6's rate 5", method.Throughput)
	}
}

// k6 reports http_req_failed as a Rate metric — passes/fails and a "value"
// ratio, not the "rate" field a Counter carries. Reading the wrong field made
// every run look 100% successful.
func TestExtractMethodFromSummary_FailureRateForms(t *testing.T) {
	tests := []struct {
		name       string
		failed     k6MetricValue
		wantErrors int64
		wantRate   float64
	}{
		{"passes and fails", k6MetricValue{Passes: 25, Fails: 75}, 25, 25},
		{"value ratio", k6MetricValue{Value: 0.5}, 50, 50},
		{"rate field", k6MetricValue{Rate: 0.1}, 10, 10},
		{"values map", k6MetricValue{Values: map[string]float64{"value": 0.2}}, 20, 20},
		{"no failures", k6MetricValue{Passes: 0, Fails: 100}, 0, 0},
	}
	for _, tc := range tests {
		summary := &k6Summary{Metrics: map[string]k6MetricValue{
			"http_req_duration{scenario:geth,req_name:eth_call}": {Avg: 10, Min: 1, Max: 20},
			"http_reqs{scenario:geth,req_name:eth_call}":         {Count: 100, Rate: 10},
			"http_req_failed{scenario:geth,req_name:eth_call}":   tc.failed,
		}}

		method := extractMethodFromSummary(summary, "req_name", "geth", "eth_call")
		if method == nil {
			t.Fatalf("%s: expected a method summary", tc.name)
		}
		if method.ErrorCount != tc.wantErrors {
			t.Errorf("%s: ErrorCount = %d, want %d", tc.name, method.ErrorCount, tc.wantErrors)
		}
		if method.ErrorRate != tc.wantRate {
			t.Errorf("%s: ErrorRate = %v, want %v", tc.name, method.ErrorRate, tc.wantRate)
		}
		if want := 100 - tc.wantRate; method.SuccessRate != want {
			t.Errorf("%s: SuccessRate = %v, want %v", tc.name, method.SuccessRate, want)
		}
	}
}
