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

		for _, series := range client.Series {
			labels := withLabels(base, map[string]string{
				"req_name":   series.Name,
				"rpc_method": series.Method,
				"status":     strconv.Itoa(series.Status),
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

			// The outcome label is what makes an RPC error visible in a query.
			// It sits on the counters rather than the trends: the trends are
			// already split by status, and a second splitting label would
			// multiply series without a consumer.
			for _, outcome := range Outcomes {
				if n := series.Outcomes[outcome]; n > 0 {
					add("http_reqs_total", withLabels(labels, map[string]string{"outcome": string(outcome)}), float64(n))
				}
			}

			// Preserved with k6's meaning: HTTP-level failure only, so the
			// panel written against it keeps reporting what it always did.
			add("http_req_failed_rate", labels, ratio(series.HTTPFails, series.Count))
			add("rpc_error_rate", labels, ratio(series.RPCErrors, series.Count))

			for code, n := range series.RPCCodes {
				add("rpc_errors_total",
					withLabels(labels, map[string]string{"rpc_code": strconv.Itoa(code)}),
					float64(n))
			}
		}

		for _, stat := range trendStats {
			add("iteration_duration_"+stat.suffix, base, secondsOf(stat.value(client.Duration)))
			add("queue_delay_"+stat.suffix, base, secondsOf(stat.value(client.QueueDelay)))
		}

		add("iterations_total", base, float64(client.Count))
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
		add("inflight_max", base, float64(client.Delivery.InflightPeak))
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
