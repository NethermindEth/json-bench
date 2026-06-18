package metrics

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/sirupsen/logrus"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/types"
)

type k6MetricValue struct {
	Count  int64              `json:"count"`
	Rate   float64            `json:"rate"`
	Avg    float64            `json:"avg"`
	Min    float64            `json:"min"`
	Max    float64            `json:"max"`
	Med    float64            `json:"med"`
	P90    float64            `json:"p(90)"`
	P95    float64            `json:"p(95)"`
	P99    float64            `json:"p(99)"`
	StdDev float64            `json:"std_dev"`
	Values map[string]float64 `json:"values"`
}

type k6Summary struct {
	Metrics map[string]k6MetricValue `json:"metrics"`
}

func loadK6Summary(path string) (*k6Summary, error) {
	if path == "" {
		return nil, fmt.Errorf("summary path is empty")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read summary file: %w", err)
	}
	var s k6Summary
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("failed to parse summary: %w", err)
	}
	return &s, nil
}

func applySummaryFallback(clientsMetrics map[string]*types.ClientMetrics, cfg *config.Config, summaryPath string, logger *logrus.Logger) {
	missing := collectMissingPairs(clientsMetrics, cfg)
	if len(missing) == 0 {
		return
	}

	var (
		summary   *k6Summary
		loadErr   error
		loadTried bool
	)
	loadSummary := func() *k6Summary {
		if loadTried {
			return summary
		}
		loadTried = true
		summary, loadErr = loadK6Summary(summaryPath)
		if loadErr != nil {
			logger.WithError(loadErr).Warnf("Cannot read k6 summary at %s; fallback will leave missing metrics at zero", summaryPath)
		}
		return summary
	}

	for _, pair := range missing {
		logger.Warnf("Prometheus had no data for %s.%s, falling back to summary.json", pair.client, pair.method)
		s := loadSummary()
		if s == nil {
			continue
		}
		client := clientsMetrics[pair.client]
		if client == nil {
			continue
		}
		method := extractMethodFromSummary(s, pair.client, pair.method)
		if method == nil {
			continue
		}
		client.Methods[pair.method] = *method
	}
}

type clientMethodPair struct {
	client string
	method string
}

func collectMissingPairs(clientsMetrics map[string]*types.ClientMetrics, cfg *config.Config) []clientMethodPair {
	if cfg == nil {
		return nil
	}
	missing := make([]clientMethodPair, 0)
	for _, client := range cfg.ResolvedClients {
		cm, ok := clientsMetrics[client.Name]
		if !ok {
			continue
		}
		for _, call := range cfg.Calls {
			methodName := call.Name
			if methodName == "" {
				methodName = call.Method
			}
			if _, exists := cm.Methods[methodName]; exists {
				continue
			}
			missing = append(missing, clientMethodPair{client: client.Name, method: methodName})
		}
	}
	return missing
}

func extractMethodFromSummary(s *k6Summary, clientName, methodName string) *types.MetricSummary {
	if s == nil {
		return nil
	}
	callsKey := fmt.Sprintf("client_%s_method_calls_%s", clientName, methodName)
	latencyKey := fmt.Sprintf("client_%s_method_latency_%s", clientName, methodName)
	errorsKey := fmt.Sprintf("client_%s_method_errors_%s", clientName, methodName)
	successKey := fmt.Sprintf("client_%s_method_success_%s", clientName, methodName)

	calls, hasCalls := s.Metrics[callsKey]
	latency, hasLatency := s.Metrics[latencyKey]
	if !hasCalls && !hasLatency {
		return nil
	}

	method := types.MetricSummary{}
	if hasCalls {
		method.Count = calls.Count
	}
	if hasLatency {
		method.Min = latency.Min
		method.Max = latency.Max
		method.Avg = latency.Avg
		method.P50 = latency.Med
		method.P90 = latency.P90
		method.P95 = latency.P95
		method.P99 = latency.P99
		method.StdDev = (latency.Max - latency.Min) / 4
		if method.Avg > 0 {
			method.CoeffVar = (method.StdDev / method.Avg) * 100
		}
	}
	if errors, ok := s.Metrics[errorsKey]; ok {
		method.ErrorCount = errors.Count
	}
	if success, ok := s.Metrics[successKey]; ok {
		method.SuccessCount = success.Count
	}

	if method.Count > 0 {
		if method.SuccessCount == 0 && method.ErrorCount > 0 {
			method.SuccessCount = method.Count - method.ErrorCount
		} else if method.ErrorCount == 0 && method.SuccessCount > 0 {
			method.ErrorCount = method.Count - method.SuccessCount
		}
		if method.ErrorCount > 0 {
			method.ErrorRate = float64(method.ErrorCount) / float64(method.Count) * 100
		}
		method.SuccessRate = 100.0 - method.ErrorRate
	}

	return &method
}
