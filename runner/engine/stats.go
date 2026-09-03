package engine

import (
	"sort"
	"strconv"
	"time"

	"github.com/jsonrpc-bench/runner/metrics"
	"github.com/jsonrpc-bench/runner/types"
)

// calc computes the statistics that k6's aggregates could not supply. Its
// percentile uses linear interpolation over the sorted samples, matching k6's
// trend sink so the two engines' numbers are comparable.
var calc = metrics.NewAdvancedCalculator()

// group accumulates the samples for one (client, method) pair.
type group struct {
	latencies []float64
	outcomes  map[Outcome]int64
	rpcCodes  map[int]int64
	statuses  map[int]int64
	respBytes []float64
	reused    int64
	conns     int64
	dns       []float64
	tcp       []float64
	tls       []float64
}

func newGroup() *group {
	return &group{
		outcomes: make(map[Outcome]int64, len(Outcomes)),
		rpcCodes: make(map[int]int64),
		statuses: make(map[int]int64),
	}
}

func (g *group) add(s Sample) {
	g.latencies = append(g.latencies, msOf(s.Service()))
	g.outcomes[s.Outcome]++
	g.statuses[s.Status]++
	g.respBytes = append(g.respBytes, float64(s.ResponseBytes))
	if s.Outcome == OutcomeRPCError {
		g.rpcCodes[s.RPCCode]++
	}
	if s.ConnectionReused {
		g.reused++
	} else {
		g.conns++
	}
	if s.Phases.DNS > 0 {
		g.dns = append(g.dns, msOf(s.Phases.DNS))
	}
	if s.Phases.Connecting > 0 {
		g.tcp = append(g.tcp, msOf(s.Phases.Connecting))
	}
	if s.Phases.TLS > 0 {
		g.tls = append(g.tls, msOf(s.Phases.TLS))
	}
}

func (g *group) count() int64 {
	var n int64
	for _, c := range g.outcomes {
		n += c
	}
	return n
}

func (g *group) errors() int64 {
	var n int64
	for outcome, c := range g.outcomes {
		if outcome.IsError() {
			n += c
		}
	}
	return n
}

// summarize turns retained samples into a MetricSummary. Every field is
// computed from the samples, including the ones the k6 pipeline left at zero or
// filled with a stand-in.
func (g *group) summarize(elapsedSeconds float64) types.MetricSummary {
	count := g.count()
	if count == 0 {
		return types.MetricSummary{}
	}

	values := append([]float64(nil), g.latencies...)
	sort.Float64s(values)

	mean := meanOf(values)
	variance := calc.CalculateVariance(values, mean)
	stdDev := calc.CalculateStdDev(variance)

	errCount := g.errors()
	summary := types.MetricSummary{
		Count:            count,
		Min:              values[0],
		Max:              values[len(values)-1],
		Avg:              mean,
		P50:              calc.CalculatePercentile(values, 50),
		P75:              calc.CalculatePercentile(values, 75),
		P90:              calc.CalculatePercentile(values, 90),
		P95:              calc.CalculatePercentile(values, 95),
		P99:              calc.CalculatePercentile(values, 99),
		P999:             calc.CalculatePercentile(values, 99.9),
		StdDev:           stdDev,
		Variance:         variance,
		Skewness:         calc.CalculateSkewness(values, mean, stdDev),
		Kurtosis:         calc.CalculateKurtosis(values, mean, stdDev),
		CoeffVar:         calc.CalculateCoeffVar(mean, stdDev),
		IQR:              calc.CalculateIQR(values),
		MAD:              calc.CalculateMAD(values),
		ErrorCount:       errCount,
		SuccessCount:     count - errCount,
		ErrorRate:        percent(errCount, count),
		SuccessRate:      percent(count-errCount, count),
		TimeoutRate:      percent(g.outcomes[OutcomeTimeout], count),
		ConnectionErrors: g.outcomes[OutcomeTransport],
	}
	if elapsedSeconds > 0 {
		summary.Throughput = float64(count) / elapsedSeconds
	}
	return summary
}

// Result is what a run produced, before it is projected onto the report types.
type Result struct {
	Samples    map[string][]Sample
	Deliveries map[string]Delivery
	SamplePath string
}

// clientMetrics projects one client's samples onto the report type. The
// client-level latency summary is computed from that client's pooled samples,
// not by averaging the per-method percentiles, which is not a percentile of
// anything.
func clientMetrics(client *target, samples []Sample, delivery Delivery) *types.ClientMetrics {
	elapsed := delivery.Elapsed().Seconds()

	overall := newGroup()
	byMethod := make(map[string]*group)
	for _, s := range samples {
		overall.add(s)
		g, ok := byMethod[s.Method]
		if !ok {
			g = newGroup()
			byMethod[s.Method] = g
		}
		g.add(s)
	}

	cm := &types.ClientMetrics{
		Name:          client.name,
		Methods:       make(map[string]types.MetricSummary, len(byMethod)),
		MethodDetails: make(map[string]*types.MethodMetrics, len(byMethod)),
		ErrorTypes:    make(map[string]int64, len(Outcomes)),
		StatusCodes:   make(map[int]int64, len(overall.statuses)),
		TotalRequests: overall.count(),
		TotalErrors:   overall.errors(),
		Latency:       overall.summarize(elapsed),
		TimeSeries:    make(map[string][]types.TimeSeriesPoint),
	}
	cm.ErrorRate = cm.Latency.ErrorRate

	for method, g := range byMethod {
		summary := g.summarize(elapsed)
		cm.Methods[method] = summary
		cm.MethodDetails[method] = &types.MethodMetrics{MetricSummary: summary, Name: method}
	}

	// ErrorTypes is keyed on the outcome class and, for JSON-RPC failures, on
	// the code the node returned. k6 keyed it on its own opaque numeric
	// taxonomy, which told an operator nothing.
	for outcome, n := range overall.outcomes {
		if outcome.IsError() {
			cm.ErrorTypes[string(outcome)] = n
		}
	}
	for code, n := range overall.rpcCodes {
		cm.ErrorTypes["rpc_code_"+itoa(code)] = n
	}
	for status, n := range overall.statuses {
		if status > 0 {
			cm.StatusCodes[status] += n
		}
	}

	total := overall.reused + overall.conns
	cm.ConnectionMetrics = types.ConnectionMetrics{
		ConnectionsCreated: overall.conns,
		ConnectionPoolSize: 0,
		ConnectionTimeouts: overall.outcomes[OutcomeTimeout],
		DNSResolutionTime:  meanOf(overall.dns),
		TCPHandshakeTime:   meanOf(overall.tcp),
		TLSHandshakeTime:   meanOf(overall.tls),
	}
	if total > 0 {
		cm.ConnectionMetrics.ConnectionReuse = float64(overall.reused) / float64(total) * 100
	}

	return cm
}

func meanOf(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func percent(part, whole int64) float64 {
	if whole == 0 {
		return 0
	}
	return float64(part) / float64(whole) * 100
}

func msOf(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

func itoa(v int) string { return strconv.Itoa(v) }
