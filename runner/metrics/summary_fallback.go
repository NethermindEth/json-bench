package metrics

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/types"
)

type k6MetricValue struct {
	Count int64   `json:"count"`
	Rate  float64 `json:"rate"`
	// A k6 Rate metric (http_req_failed) reports observations, not a "rate"
	// field: Passes counts the observations where the condition held, Fails the
	// ones where it did not, and Value is Passes/(Passes+Fails). Since the
	// condition on http_req_failed is "the request failed", Passes is the
	// failure count — see failedRate.
	Passes int64              `json:"passes"`
	Fails  int64              `json:"fails"`
	Value  float64            `json:"value"`
	Avg    float64            `json:"avg"`
	Min    float64            `json:"min"`
	Max    float64            `json:"max"`
	Med    float64            `json:"med"`
	P75    float64            `json:"p(75)"`
	P90    float64            `json:"p(90)"`
	P95    float64            `json:"p(95)"`
	P99    float64            `json:"p(99)"`
	P999   float64            `json:"p(99.9)"`
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

// runElapsedSeconds reports how long the benchmark actually ran. k6's summary
// carries no duration field, but http_reqs does carry both a count and the rate
// k6 derived from it, so count/rate recovers the real run length — which is
// what throughput must be divided by. The configured duration is the fallback,
// and it is only an upper bound for iteration-based runs that finish early.
func runElapsedSeconds(cfg *config.Config, summaryPath string, logger *logrus.Logger) float64 {
	if s, err := loadK6Summary(summaryPath); err == nil {
		if reqs, ok := s.Metrics["http_reqs"]; ok {
			count := float64(reqs.Count)
			if count == 0 {
				count = metricFloat(reqs, "count")
			}
			rate := pickFloat(reqs.Rate, metricFloat(reqs, "rate"))
			if count > 0 && rate > 0 {
				return count / rate
			}
		}
	}
	if cfg == nil {
		return 0
	}
	d, err := time.ParseDuration(cfg.Duration)
	if err != nil {
		logger.Warnf("Cannot determine the benchmark duration from %s or the config; throughput will be reported as unavailable", summaryPath)
		return 0
	}
	return d.Seconds()
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
		method := extractMethodFromSummary(s, pair.tag, pair.client, pair.method)
		if method == nil {
			continue
		}
		client.Methods[pair.method] = *method
	}
}

type clientMethodPair struct {
	client string
	method string
	// tag is the k6 tag the submetric is keyed on: req_name for a declared-calls
	// run, rpc_method for a calls-file run.
	tag string
}

func collectMissingPairs(clientsMetrics map[string]*types.ClientMetrics, cfg *config.Config) []clientMethodPair {
	if cfg == nil {
		return nil
	}
	methodTag, methodNames := cfg.MethodKeys()
	missing := make([]clientMethodPair, 0)
	for _, client := range cfg.ResolvedClients {
		cm, ok := clientsMetrics[client.Name]
		if !ok {
			continue
		}
		for _, methodName := range methodNames {
			if _, exists := cm.Methods[methodName]; exists {
				continue
			}
			missing = append(missing, clientMethodPair{client: client.Name, method: methodName, tag: methodTag})
		}
	}
	return missing
}

// lookupSubmetric finds a k6 submetric value keyed on either tag ordering
// (`{<tag>:M,scenario:C}` or `{scenario:C,<tag>:M}`). k6's tag order is
// deterministic per version but we tolerate either form to avoid binding the
// parser to a single upstream choice.
func lookupSubmetric(s *k6Summary, base, methodTag, clientName, methodName string) (k6MetricValue, bool) {
	if methodTag == "" {
		methodTag = "req_name"
	}
	candidates := [2]string{
		fmt.Sprintf("%s{%s:%s,scenario:%s}", base, methodTag, methodName, clientName),
		fmt.Sprintf("%s{scenario:%s,%s:%s}", base, clientName, methodTag, methodName),
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

func extractMethodFromSummary(s *k6Summary, methodTag, clientName, methodName string) *types.MetricSummary {
	if s == nil {
		return nil
	}

	duration, hasDuration := lookupSubmetric(s, "http_req_duration", methodTag, clientName, methodName)
	reqs, hasReqs := lookupSubmetric(s, "http_reqs", methodTag, clientName, methodName)
	if !hasDuration && !hasReqs {
		return nil
	}

	method := types.MetricSummary{}

	if hasDuration {
		method.Min = pickFloat(duration.Min, metricFloat(duration, "min"))
		method.Max = pickFloat(duration.Max, metricFloat(duration, "max"))
		method.Avg = pickFloat(duration.Avg, metricFloat(duration, "avg"))
		method.P50 = pickFloat(duration.Med, metricFloat(duration, "med"))
		method.P75 = pickFloat(duration.P75, metricFloat(duration, "p(75)"))
		method.P90 = pickFloat(duration.P90, metricFloat(duration, "p(90)"))
		method.P95 = pickFloat(duration.P95, metricFloat(duration, "p(95)"))
		method.P99 = pickFloat(duration.P99, metricFloat(duration, "p(99)"))
		method.P999 = pickFloat(duration.P999, metricFloat(duration, "p(99.9)"))
		// Range/4 is a crude stand-in: k6's summary carries no sample values, so
		// a real standard deviation is not recoverable here.
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
		// k6 computes this as count/testRunDuration, which is the throughput
		// k6 itself reports; prefer it over anything we could derive.
		method.Throughput = pickFloat(reqs.Rate, metricFloat(reqs, "rate"))
	}

	if failed, ok := lookupSubmetric(s, "http_req_failed", methodTag, clientName, methodName); ok && method.Count > 0 {
		failRate := failedRate(failed)
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

// failedRate reads the failure ratio out of k6's http_req_failed metric. It is a
// Rate metric, so the summary carries passes/fails and a "value" ratio — not the
// "rate" field a Counter carries. Reading the wrong one silently reported every
// run as 100% successful.
//
// Value is authoritative: k6 computes it as Passes/(Passes+Fails), and for this
// metric a "pass" is an observation of the condition "the request failed", so
// Value is already the failure ratio the `rate < 0.01` threshold is written
// against. Two runs against a stub, for the same 400-odd requests:
//
//	all responses 200      -> {passes: 0,   fails: 402, value: 0}
//	429 on every 4th       -> {passes: 100, fails: 301, value: 0.2494}
//
// so Passes counts failures, not successes. Passes/(Passes+Fails) is the
// fallback for a summary that omits value.
func failedRate(v k6MetricValue) float64 {
	if total := v.Passes + v.Fails; total > 0 {
		if v.Value > 0 {
			return v.Value
		}
		return float64(v.Passes) / float64(total)
	}
	return pickFloat(v.Value, pickFloat(metricFloat(v, "value"), pickFloat(v.Rate, metricFloat(v, "rate"))))
}

func pickFloat(primary, fallback float64) float64 {
	if primary != 0 {
		return primary
	}
	return fallback
}
