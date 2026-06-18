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

// lookupSubmetric finds a k6 submetric value keyed on either tag ordering
// (`{req_name:M,scenario:C}` or `{scenario:C,req_name:M}`). k6's tag order
// is deterministic per version but we tolerate either form to avoid binding
// the parser to a single upstream choice.
func lookupSubmetric(s *k6Summary, base, clientName, methodName string) (k6MetricValue, bool) {
	candidates := [2]string{
		fmt.Sprintf("%s{req_name:%s,scenario:%s}", base, methodName, clientName),
		fmt.Sprintf("%s{scenario:%s,req_name:%s}", base, clientName, methodName),
	}
	for _, k := range candidates {
		if v, ok := s.Metrics[k]; ok {
			return v, true
		}
	}
	return k6MetricValue{}, false
}

// metricFloat returns the float keyed under `name` in `values`, or 0 if
// `values` is nil or the key is absent. k6's `--summary-export` serializes
// numeric aggregates under both top-level fields (Avg, P95, ...) and a
// generic `values` map; prefer the explicit field, fall back to `values`.
func metricFloat(v k6MetricValue, valueKey string) float64 {
	if f, ok := v.Values[valueKey]; ok {
		return f
	}
	return 0
}

func extractMethodFromSummary(s *k6Summary, clientName, methodName string) *types.MetricSummary {
	if s == nil {
		return nil
	}

	duration, hasDuration := lookupSubmetric(s, "http_req_duration", clientName, methodName)
	reqs, hasReqs := lookupSubmetric(s, "http_reqs", clientName, methodName)
	if !hasDuration && !hasReqs {
		return nil
	}

	method := types.MetricSummary{}

	if hasDuration {
		method.Min = pickFloat(duration.Min, metricFloat(duration, "min"))
		method.Max = pickFloat(duration.Max, metricFloat(duration, "max"))
		method.Avg = pickFloat(duration.Avg, metricFloat(duration, "avg"))
		method.P50 = pickFloat(duration.Med, metricFloat(duration, "med"))
		method.P90 = pickFloat(duration.P90, metricFloat(duration, "p(90)"))
		method.P95 = pickFloat(duration.P95, metricFloat(duration, "p(95)"))
		method.P99 = pickFloat(duration.P99, metricFloat(duration, "p(99)"))
		method.StdDev = (method.Max - method.Min) / 4
		if method.Avg > 0 {
			method.CoeffVar = (method.StdDev / method.Avg) * 100
		}
	}
	if hasReqs {
		if reqs.Count > 0 {
			method.Count = reqs.Count
		} else if c, ok := reqs.Values["count"]; ok {
			method.Count = int64(c)
		}
	}

	if failed, ok := lookupSubmetric(s, "http_req_failed", clientName, methodName); ok && method.Count > 0 {
		failRate := pickFloat(failed.Rate, metricFloat(failed, "rate"))
		method.ErrorCount = int64(float64(method.Count)*failRate + 0.5)
		method.SuccessCount = method.Count - method.ErrorCount
		method.ErrorRate = failRate * 100
		method.SuccessRate = 100.0 - method.ErrorRate
	} else if method.Count > 0 {
		method.SuccessCount = method.Count
		method.SuccessRate = 100.0
	}

	return &method
}

func pickFloat(primary, fallback float64) float64 {
	if primary != 0 {
		return primary
	}
	return fallback
}
