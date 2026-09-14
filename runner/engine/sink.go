package engine

import (
	"context"
	"strconv"

	"github.com/jsonrpc-bench/runner/engine/promrw"
	"github.com/jsonrpc-bench/runner/types"
)

// Namespace prefixes every emitted metric. The structure below follows what k6
// established — stat suffixes on trends, _total counters, _rate gauges, seconds
// on the wire, cumulative from the run's start — so only the prefix and the
// k6-specific labels changed. metrics/METRICS.md carries the full mapping.
const Namespace = "bench"

// trendStats are the suffixes emitted for every trend family. The set matches
// the K6_PROMETHEUS_RW_TREND_STATS the runner configured, and the dashboard
// discovers its stat picker from the emitted names, so the ladder stays whole.
var trendStats = []struct {
	suffix string
	value  func(types.MetricSummary) float64
}{
	{"min", func(s types.MetricSummary) float64 { return s.Min }},
	{"max", func(s types.MetricSummary) float64 { return s.Max }},
	{"avg", func(s types.MetricSummary) float64 { return s.Avg }},
	{"med", func(s types.MetricSummary) float64 { return s.P50 }},
	{"p75", func(s types.MetricSummary) float64 { return s.P75 }},
	{"p90", func(s types.MetricSummary) float64 { return s.P90 }},
	{"p95", func(s types.MetricSummary) float64 { return s.P95 }},
	{"p99", func(s types.MetricSummary) float64 { return s.P99 }},
	{"p999", func(s types.MetricSummary) float64 { return s.P999 }},
}

// byteStats is the reduced ladder for size distributions. A response size does
// not need the full percentile ladder, and the full one would triple the
// cardinality of a family no stat picker reads.
var byteStats = []struct {
	suffix string
	value  func(types.MetricSummary) float64
}{
	{"avg", func(s types.MetricSummary) float64 { return s.Avg }},
	{"max", func(s types.MetricSummary) float64 { return s.Max }},
	{"p95", func(s types.MetricSummary) float64 { return s.P95 }},
}

// PromSink publishes snapshots to a Prometheus remote-write endpoint.
type PromSink struct {
	client *promrw.Client
}

func NewPromSink(client *promrw.Client) *PromSink {
	return &PromSink{client: client}
}

func (s *PromSink) Push(ctx context.Context, snap Snapshot) error {
	return s.client.Push(ctx, BuildSeries(snap))
}

func (s *PromSink) Close(context.Context) error { return nil }

// BuildSeries renders a snapshot as remote-write series.
//
// Durations go out in seconds because that is the Prometheus base unit and what
// k6 wrote, while the reports stay in milliseconds. Trend values are cumulative
// over the run so far, not windowed: every latency panel is written as an
// aggregation over the raw gauge, and a windowed value would silently change
// what those panels mean.
func BuildSeries(snap Snapshot) []promrw.Series {
	ts := snap.At.UnixMilli()
	var out []promrw.Series

	add := func(name string, labels map[string]string, value float64) {
		out = append(out, promrw.Series{
			Name:        Namespace + "_" + name,
			Labels:      labels,
			Value:       value,
			TimestampMS: ts,
		})
	}

	for _, client := range snap.Clients {
		base := map[string]string{
			"testid":   snap.TestName,
			"scenario": client.Name,
		}
		if client.ClientType != "" {
			base["client_type"] = client.ClientType
		}

		// Distributions and counters carry the outcome, so the latency of the
		// calls that succeeded can be asked for separately from the calls that
		// failed. Mixing them understates a client that fails fast.
		for _, series := range client.Series {
			labels := withLabels(base, map[string]string{
				"req_name":   series.Name,
				"rpc_method": series.Method,
				"status":     strconv.Itoa(series.Status),
				"outcome":    string(series.Outcome),
			})

			for _, phase := range PhaseNames {
				summary := series.Phases[phase]
				for _, stat := range trendStats {
					add("http_req_"+string(phase)+"_"+stat.suffix, labels, secondsOf(stat.value(summary)))
				}
			}

			for _, stat := range byteStats {
				add("resp_bytes_"+stat.suffix, labels, stat.value(series.RespBytes))
			}

			add("http_reqs_total", labels, float64(series.Count))
		}

		// The ratio families are per method, merged across status and outcome.
		// A failure rate inside one status bucket is 0 or 1, so k6's
		// per-status http_req_failed could not be averaged into anything
		// meaningful — which is what its dashboard panel tried to do.
		for _, totals := range client.Totals {
			labels := withLabels(base, map[string]string{
				"req_name":   totals.Name,
				"rpc_method": totals.Method,
			})

			add("http_req_failed_rate", labels, ratio(totals.HTTPFails, totals.Count))
			add("rpc_error_rate", labels, ratio(totals.RPCErrors, totals.Count))

			for code, n := range totals.RPCCodes {
				add("rpc_errors_total",
					withLabels(labels, map[string]string{"rpc_code": strconv.Itoa(code)}),
					float64(n))
			}
		}

		for _, stat := range trendStats {
			add("iteration_duration_"+stat.suffix, base, secondsOf(stat.value(client.Duration)))
			add("queue_delay_"+stat.suffix, base, secondsOf(stat.value(client.QueueDelay)))
		}

		// One iteration is one HTTP round trip: a batch when batching, a request
		// otherwise. bench_http_reqs_total counts the calls inside them.
		add("iterations_total", base, float64(client.Iterations))
		add("data_sent_total", base, client.ReqBytes)
		add("data_received_total", base, client.RespBytes)

		// The delivery family has no k6 counterpart beyond dropped_iterations.
		// Without it a run that offered a fraction of its target rate reads
		// exactly like one that met it.
		add("requests_scheduled_total", base, float64(client.Delivery.Scheduled))
		add("requests_late_total", base, float64(client.Delivery.Late))
		add("requests_dropped_total", base, float64(client.Delivery.Dropped))
		add("achieved_rate", base, client.Delivery.AchievedRate())
		add("inflight", base, float64(client.Delivery.Inflight))
		add("inflight_peak", base, float64(client.Delivery.InflightPeak))

		// The node's own metrics, republished under this namespace so they sit
		// on the same timeline as the latency above. The names are prefixed
		// rather than passed through, so a series that reached Prometheus by
		// way of a benchmark is distinguishable from one scraped directly, and
		// cannot collide with it.
		for _, point := range client.Target {
			// The run's identity labels are applied last, so a node metric
			// that happens to carry a label called scenario or testid cannot
			// overwrite which client the sample came from.
			add("target_"+point.Name, withLabels(point.Labels, base), point.Value)
		}
	}

	return out
}

func withLabels(base, extra map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(extra))
	for name, value := range base {
		out[name] = value
	}
	for name, value := range extra {
		out[name] = value
	}
	return out
}

func secondsOf(milliseconds float64) float64 { return milliseconds / 1000 }

func ratio(part, whole int64) float64 {
	if whole == 0 {
		return 0
	}
	return float64(part) / float64(whole)
}
