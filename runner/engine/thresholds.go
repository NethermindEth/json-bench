package engine

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/jsonrpc-bench/runner/types"
)

// thresholdPattern matches the k6 threshold expressions the committed configs
// use: a statistic, a comparison and a number, e.g. "p(99)<600000". Whitespace
// is optional because both forms appear in the wild.
var thresholdPattern = regexp.MustCompile(`^\s*([a-z]+(?:\(\s*[0-9.]+\s*\))?)\s*(<=|>=|<|>)\s*([0-9.]+)\s*$`)

// Threshold is one parsed pass/fail condition on a metric summary.
type Threshold struct {
	Target     string
	Expression string

	stat  string
	op    string
	limit float64
}

// ParseThreshold reads a k6 threshold expression. Latency statistics are in
// milliseconds and `rate` is a fraction, matching how k6 evaluated them.
func ParseThreshold(target, expression string) (Threshold, error) {
	match := thresholdPattern.FindStringSubmatch(expression)
	if match == nil {
		return Threshold{}, fmt.Errorf("cannot parse threshold %q (want a form like \"p(99)<600\")", expression)
	}

	stat := strings.ReplaceAll(match[1], " ", "")
	if _, err := statValue(stat, types.MetricSummary{}); err != nil {
		return Threshold{}, fmt.Errorf("threshold %q: %w", expression, err)
	}

	limit, err := strconv.ParseFloat(match[3], 64)
	if err != nil {
		return Threshold{}, fmt.Errorf("threshold %q has an unparseable limit: %w", expression, err)
	}

	return Threshold{
		Target:     target,
		Expression: strings.TrimSpace(expression),
		stat:       stat,
		op:         match[2],
		limit:      limit,
	}, nil
}

// Evaluate reports whether the summary satisfies the threshold, along with the
// value that was compared.
func (t Threshold) Evaluate(summary types.MetricSummary) (bool, float64, error) {
	value, err := statValue(t.stat, summary)
	if err != nil {
		return false, 0, err
	}
	switch t.op {
	case "<":
		return value < t.limit, value, nil
	case "<=":
		return value <= t.limit, value, nil
	case ">":
		return value > t.limit, value, nil
	case ">=":
		return value >= t.limit, value, nil
	}
	return false, value, fmt.Errorf("unsupported comparison %q", t.op)
}

// statValue resolves a threshold statistic against a summary. `rate` is the
// error rate as a fraction, since that is the scale k6's thresholds were
// written against, while the summary carries it as a percentage.
func statValue(stat string, summary types.MetricSummary) (float64, error) {
	if percentile, ok := parsePercentileStat(stat); ok {
		switch percentile {
		case 50:
			return summary.P50, nil
		case 75:
			return summary.P75, nil
		case 90:
			return summary.P90, nil
		case 95:
			return summary.P95, nil
		case 99:
			return summary.P99, nil
		case 99.9:
			return summary.P999, nil
		default:
			return 0, fmt.Errorf("unsupported percentile p(%g); the engine records 50, 75, 90, 95, 99 and 99.9", percentile)
		}
	}

	switch stat {
	case "avg":
		return summary.Avg, nil
	case "min":
		return summary.Min, nil
	case "max":
		return summary.Max, nil
	case "med":
		return summary.P50, nil
	case "count":
		return float64(summary.Count), nil
	case "rate":
		return summary.ErrorRate / 100, nil
	default:
		return 0, fmt.Errorf("unknown statistic %q", stat)
	}
}

func parsePercentileStat(stat string) (float64, bool) {
	if !strings.HasPrefix(stat, "p(") || !strings.HasSuffix(stat, ")") {
		return 0, false
	}
	value, err := strconv.ParseFloat(stat[2:len(stat)-1], 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

// ThresholdBreach records a threshold that failed, for the operator and for the
// exit code.
type ThresholdBreach struct {
	Client     string
	Target     string
	Expression string
	Actual     float64
}

func (b ThresholdBreach) String() string {
	return fmt.Sprintf("%s/%s: %s (actual %.2f)", b.Client, b.Target, b.Expression, b.Actual)
}

// CollectThresholds parses the per-call thresholds declared in the config,
// keyed by the identifier the per-method breakdown uses.
func CollectThresholds(calls []*callThresholds) ([]Threshold, error) {
	var out []Threshold
	for _, call := range calls {
		for _, expression := range call.expressions {
			threshold, err := ParseThreshold(call.target, expression)
			if err != nil {
				return nil, err
			}
			out = append(out, threshold)
		}
	}
	return out, nil
}

type callThresholds struct {
	target      string
	expressions []string
}

// EvaluateThresholds checks every threshold against the client metrics it
// targets, returning the breaches. A threshold whose target saw no traffic is
// reported as a breach: silently passing a condition on a method that never ran
// is how an empty run looks like a successful one.
func EvaluateThresholds(thresholds []Threshold, clients map[string]*types.ClientMetrics) ([]ThresholdBreach, error) {
	var breaches []ThresholdBreach
	for name, client := range clients {
		for _, threshold := range thresholds {
			summary, ok := client.Methods[threshold.Target]
			if !ok {
				breaches = append(breaches, ThresholdBreach{
					Client: name, Target: threshold.Target,
					Expression: threshold.Expression + " (no requests recorded)",
				})
				continue
			}
			pass, actual, err := threshold.Evaluate(summary)
			if err != nil {
				return nil, err
			}
			if !pass {
				breaches = append(breaches, ThresholdBreach{
					Client: name, Target: threshold.Target,
					Expression: threshold.Expression, Actual: actual,
				})
			}
		}
	}
	return breaches, nil
}
