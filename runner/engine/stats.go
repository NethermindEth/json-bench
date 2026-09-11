package engine

import (
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/jsonrpc-bench/runner/metrics"
	"github.com/jsonrpc-bench/runner/types"
)

// calc computes the statistics k6's aggregates could not supply. Its percentile
// uses linear interpolation over the sorted samples, matching k6's trend sink so
// the two engines' numbers stay comparable.
var calc = metrics.NewAdvancedCalculator()

// Phase names the latency component a trend family reports, using k6's names so
// a reader moving between the two finds the same decomposition.
type Phase string

const (
	PhaseDuration   Phase = "duration"
	PhaseBlocked    Phase = "blocked"
	PhaseConnecting Phase = "connecting"
	PhaseTLS        Phase = "tls_handshaking"
	PhaseSending    Phase = "sending"
	PhaseWaiting    Phase = "waiting"
	PhaseReceiving  Phase = "receiving"
)

// Phases lists the families in emission order.
var PhaseNames = []Phase{
	PhaseDuration, PhaseBlocked, PhaseConnecting, PhaseTLS,
	PhaseSending, PhaseWaiting, PhaseReceiving,
}

// group accumulates the observations behind one series identity.
type group struct {
	// countersOnly suppresses the retained observations. Every sample already
	// lands in a keyed group, so a client-wide group that kept them too would
	// double the engine's memory for a distribution that can be concatenated
	// back from the keys — and on an on-host run the generator's footprint is
	// inside the measurement.
	countersOnly bool

	phases map[Phase][]float64
	// dns is kept for the connection report even though no series family
	// carries it: k6 folds DNS into blocked rather than emitting its own trend.
	dns       []float64
	respBytes []float64
	outcomes  map[Outcome]int64
	rpcCodes  map[int]int64
	statuses  map[int]int64
	reused    int64
	dialed    int64
}

func newCountersGroup() *group {
	g := newGroup()
	g.countersOnly = true
	return g
}

func newGroup() *group {
	return &group{
		phases:   make(map[Phase][]float64, len(PhaseNames)),
		outcomes: make(map[Outcome]int64, len(Outcomes)),
		rpcCodes: make(map[int]int64),
		statuses: make(map[int]int64),
	}
}

func (g *group) add(s Sample) {
	if g.countersOnly {
		g.addCounters(s)
		return
	}
	g.phases[PhaseDuration] = append(g.phases[PhaseDuration], msOf(s.Service()))
	g.phases[PhaseBlocked] = append(g.phases[PhaseBlocked], msOf(s.Phases.Blocked))
	g.phases[PhaseConnecting] = append(g.phases[PhaseConnecting], msOf(s.Phases.Connecting))
	g.phases[PhaseTLS] = append(g.phases[PhaseTLS], msOf(s.Phases.TLS))
	g.phases[PhaseSending] = append(g.phases[PhaseSending], msOf(s.Phases.Sending))
	g.phases[PhaseWaiting] = append(g.phases[PhaseWaiting], msOf(s.Phases.Waiting))
	g.phases[PhaseReceiving] = append(g.phases[PhaseReceiving], msOf(s.Phases.Receiving))

	g.dns = append(g.dns, msOf(s.Phases.DNS))
	g.respBytes = append(g.respBytes, float64(s.ResponseBytes))
	g.addCounters(s)
}

func (g *group) addCounters(s Sample) {
	g.outcomes[s.Outcome]++
	g.statuses[s.Status]++
	if s.Outcome == OutcomeRPCError {
		g.rpcCodes[s.RPCCode]++
	}
	if s.ConnectionReused {
		g.reused++
	} else {
		g.dialed++
	}
}

func (g *group) merge(other *group) {
	for phase, values := range other.phases {
		g.phases[phase] = append(g.phases[phase], values...)
	}
	g.dns = append(g.dns, other.dns...)
	g.respBytes = append(g.respBytes, other.respBytes...)
	for outcome, n := range other.outcomes {
		g.outcomes[outcome] += n
	}
	for code, n := range other.rpcCodes {
		g.rpcCodes[code] += n
	}
	for status, n := range other.statuses {
		g.statuses[status] += n
	}
	g.reused += other.reused
	g.dialed += other.dialed
}

func (g *group) clone() *group {
	out := newGroup()
	out.countersOnly = g.countersOnly
	out.merge(g)
	return out
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

// httpFailures counts only the HTTP-level failures, which is the population
// k6's http_req_failed described. Kept distinct from the error count so the
// series that preserves that meaning stays available alongside the RPC-aware one.
func (g *group) httpFailures() int64 {
	return g.outcomes[OutcomeHTTPError] + g.outcomes[OutcomeTimeout] +
		g.outcomes[OutcomeTransport] + g.outcomes[OutcomeTruncated]
}

// summarize turns the retained observations into a MetricSummary. Every field
// is computed from the samples, including the ones the k6 pipeline left at zero
// or filled with a stand-in.
func (g *group) summarize(elapsedSeconds float64) types.MetricSummary {
	return g.summarizeOver(g.phases[PhaseDuration], elapsedSeconds)
}

// summarizeOver attaches this group's counters to a distribution held elsewhere,
// which is how the client-wide summary is built without a second copy of every
// observation.
func (g *group) summarizeOver(values []float64, elapsedSeconds float64) types.MetricSummary {
	summary := summarizeValues(values)
	count := g.count()
	if count == 0 {
		return types.MetricSummary{}
	}

	errCount := g.errors()
	summary.Count = count
	summary.ErrorCount = errCount
	summary.SuccessCount = count - errCount
	summary.ErrorRate = percent(errCount, count)
	summary.SuccessRate = percent(count-errCount, count)
	summary.TimeoutRate = percent(g.outcomes[OutcomeTimeout], count)
	summary.ConnectionErrors = g.outcomes[OutcomeTransport]
	if elapsedSeconds > 0 {
		summary.Throughput = float64(count) / elapsedSeconds
	}
	return summary
}

// summarizeValues computes the distribution statistics for one set of
// observations. Percentiles are exact rather than an average of aggregates.
func summarizeValues(observations []float64) types.MetricSummary {
	if len(observations) == 0 {
		return types.MetricSummary{}
	}

	values := append([]float64(nil), observations...)
	sort.Float64s(values)

	mean := meanOf(values)
	variance := calc.CalculateVariance(values, mean)
	stdDev := calc.CalculateStdDev(variance)

	return types.MetricSummary{
		Count:    int64(len(values)),
		Min:      values[0],
		Max:      values[len(values)-1],
		Avg:      mean,
		P50:      calc.CalculatePercentile(values, 50),
		P75:      calc.CalculatePercentile(values, 75),
		P90:      calc.CalculatePercentile(values, 90),
		P95:      calc.CalculatePercentile(values, 95),
		P99:      calc.CalculatePercentile(values, 99),
		P999:     calc.CalculatePercentile(values, 99.9),
		StdDev:   stdDev,
		Variance: variance,
		Skewness: calc.CalculateSkewness(values, mean, stdDev),
		Kurtosis: calc.CalculateKurtosis(values, mean, stdDev),
		CoeffVar: calc.CalculateCoeffVar(mean, stdDev),
		IQR:      calc.CalculateIQR(values),
		MAD:      calc.CalculateMAD(values),
	}
}

// seriesKey is the identity distributions and counters are grouped by. It
// follows k6's grouping minus url and group, neither of which any dashboard
// panel reads, plus the outcome — so "p99 of the calls that succeeded" is a
// query rather than an unanswerable question. A client that fails fast on a
// third of its calls otherwise reports a flattering p99.
type seriesKey struct {
	name    string
	method  string
	status  int
	outcome Outcome
}

type clientAccum struct {
	clientType string
	overall    *group
	keyed      map[seriesKey]*group
	queue      []float64

	// iterations holds one duration per HTTP round trip. It differs from the
	// per-request view whenever requests are batched: a batch of ten records
	// ten equal request latencies but one iteration.
	iterations []float64
}

// Accumulator is the run's single aggregation point: the report and every
// remote-write push read from it, so a dashboard and a CSV cannot disagree.
type Accumulator struct {
	mu      sync.Mutex
	clients map[string]*clientAccum
}

func NewAccumulator() *Accumulator {
	return &Accumulator{clients: make(map[string]*clientAccum)}
}

func (a *Accumulator) Add(s Sample) {
	a.mu.Lock()
	defer a.mu.Unlock()

	client, ok := a.clients[s.Client]
	if !ok {
		client = &clientAccum{
			clientType: s.ClientType,
			overall:    newCountersGroup(),
			keyed:      make(map[seriesKey]*group),
		}
		a.clients[s.Client] = client
	}

	key := seriesKey{name: s.Name, method: s.Method, status: s.Status, outcome: s.Outcome}
	g, ok := client.keyed[key]
	if !ok {
		g = newGroup()
		client.keyed[key] = g
	}
	g.add(s)
	client.overall.add(s)
	client.queue = append(client.queue, msOf(s.Queue))
	if s.BatchLeader || s.BatchSize <= 1 {
		client.iterations = append(client.iterations, msOf(s.Service()))
	}
}

func groupOutcomes(g *group) map[string]int64 {
	out := make(map[string]int64, len(Outcomes))
	for _, outcome := range Outcomes {
		if n := g.outcomes[outcome]; n > 0 {
			out[string(outcome)] = n
		}
	}
	return out
}

// ClientMetrics projects one client's observations onto the report type. The
// client-level latency summary comes from that client's pooled samples, not
// from averaging the per-method percentiles, which is not a percentile of any
// distribution.
func (a *Accumulator) ClientMetrics(name string, delivery Delivery) *types.ClientMetrics {
	a.mu.Lock()
	defer a.mu.Unlock()

	client, ok := a.clients[name]
	if !ok {
		return &types.ClientMetrics{
			Name:        name,
			Methods:     map[string]types.MetricSummary{},
			ErrorTypes:  map[string]int64{},
			StatusCodes: map[int]int64{},
			TimeSeries:  map[string][]types.TimeSeriesPoint{},
		}
	}

	elapsed := delivery.Elapsed().Seconds()

	byMethod := make(map[string]*group)
	for key, g := range client.keyed {
		merged, ok := byMethod[key.method]
		if !ok {
			merged = newGroup()
			byMethod[key.method] = merged
		}
		merged.merge(g)
	}

	cm := &types.ClientMetrics{
		Name:          name,
		Methods:       make(map[string]types.MetricSummary, len(byMethod)),
		MethodDetails: make(map[string]*types.MethodMetrics, len(byMethod)),
		ErrorTypes:    make(map[string]int64, len(Outcomes)),
		StatusCodes:   make(map[int]int64, len(client.overall.statuses)),
		TotalRequests: client.overall.count(),
		TotalErrors:   client.overall.errors(),
		Latency:       client.overall.summarizeOver(client.phaseValues(PhaseDuration), elapsed),
		TimeSeries:    make(map[string][]types.TimeSeriesPoint),
	}
	cm.ErrorRate = cm.Latency.ErrorRate

	// Every class, successes included: a reader has to be able to see that a
	// run was fast because the node returned nothing.
	cm.Outcomes = make(map[string]int64, len(Outcomes))
	for _, outcome := range Outcomes {
		if n := client.overall.outcomes[outcome]; n > 0 {
			cm.Outcomes[string(outcome)] = n
		}
	}

	cm.Delivery = types.DeliveryMetrics{
		Scheduled:          int64(delivery.Scheduled),
		Sent:               int64(delivery.Sent),
		Late:               int64(delivery.Late),
		Dropped:            int64(delivery.Dropped),
		AchievedRPS:        delivery.AchievedRate(),
		MaxDispatchDelayMs: msOf(delivery.MaxQueueDelay()),
		InflightPeak:       delivery.InflightPeak,
		ElapsedSeconds:     elapsed,
		OfferedSeconds:     delivery.OfferedWindow().Seconds(),
		WarmupSent:         int64(delivery.WarmupSent),
	}

	for method, g := range byMethod {
		summary := g.summarize(elapsed)
		cm.Methods[method] = summary

		cm.MethodDetails[method] = &types.MethodMetrics{
			MetricSummary: summary,
			Name:          method,
			Method:        method,
			Outcomes:      groupOutcomes(g),
		}
	}

	// The call name is a second, independent breakdown: several calls can drive
	// one method with different parameters, and then the method key alone hides
	// what the run was built to measure.
	byCall := make(map[string]*group)
	callMethod := make(map[string]string)
	for key, g := range client.keyed {
		merged, ok := byCall[key.name]
		if !ok {
			merged = newGroup()
			byCall[key.name] = merged
		}
		merged.merge(g)
		callMethod[key.name] = key.method
	}
	cm.Calls = make(map[string]*types.MethodMetrics, len(byCall))
	for name, g := range byCall {
		cm.Calls[name] = &types.MethodMetrics{
			MetricSummary: g.summarize(elapsed),
			Name:          name,
			Method:        callMethod[name],
			Outcomes:      groupOutcomes(g),
		}
	}

	// ErrorTypes is keyed on the outcome class and, for JSON-RPC failures, on
	// the code the node returned. k6 keyed it on its own opaque numeric
	// taxonomy, which told an operator nothing.
	for outcome, n := range client.overall.outcomes {
		if outcome.IsError() {
			cm.ErrorTypes[string(outcome)] = n
		}
	}
	for code, n := range client.overall.rpcCodes {
		cm.ErrorTypes["rpc_code_"+strconv.Itoa(code)] = n
	}
	for status, n := range client.overall.statuses {
		if status > 0 {
			cm.StatusCodes[status] += n
		}
	}

	total := client.overall.reused + client.overall.dialed
	cm.ConnectionMetrics = types.ConnectionMetrics{
		ConnectionsCreated: client.overall.dialed,
		ConnectionTimeouts: client.overall.outcomes[OutcomeTimeout],
		// Averaged over the requests that actually dialed: a mean over every
		// request would mostly measure how often the pool was warm.
		DNSResolutionTime: meanOfNonZero(client.dnsValues()),
		TCPHandshakeTime:  meanOfNonZero(client.phaseValues(PhaseConnecting)),
		TLSHandshakeTime:  meanOfNonZero(client.phaseValues(PhaseTLS)),
	}
	if total > 0 {
		cm.ConnectionMetrics.ConnectionReuse = float64(client.overall.reused) / float64(total) * 100
	}

	return cm
}

// MethodSeries is one series identity's cumulative state at a push instant.
type MethodSeries struct {
	Name    string
	Method  string
	Status  int
	Outcome Outcome

	Phases    map[Phase]types.MetricSummary
	RespBytes types.MetricSummary
	Count     int64
}

// MethodTotals is one method's cumulative state, merged across statuses and
// outcomes. The ratio families are reported here rather than per series: a
// failure rate inside a single status bucket is 0 or 1, which is what made k6's
// http_req_failed unusable — the dashboard averaged those buckets by series
// count instead of by request count.
type MethodTotals struct {
	Name      string
	Method    string
	Count     int64
	Errors    int64
	HTTPFails int64
	RPCErrors int64
	RPCCodes  map[int]int64
}

// ClientSeries is one client's cumulative state at a push instant.
type ClientSeries struct {
	Name       string
	ClientType string
	Series     []MethodSeries
	Totals     []MethodTotals

	// Target holds what the node said about itself, read from its own metrics
	// endpoint. Republishing it here puts the node's resource use on the same
	// timeline as the latency it produced, which is the point of reading it.
	Target []types.TargetMetricPoint

	// Duration is the latency of one HTTP round trip. With batching that is
	// the batch's latency, which is what each of its callers waited.
	Duration   types.MetricSummary
	QueueDelay types.MetricSummary
	Count      int64

	// Iterations counts HTTP round trips, which is batches when batching and
	// requests otherwise.
	Iterations int64
	Errors     int64
	RespBytes  float64
	ReqBytes   float64
	Delivery   Delivery
}

// Snapshot is the whole run's cumulative state at one instant. Trend statistics
// are computed over every sample since the run began, not over the interval
// since the last push, because that is what k6 emitted and what every dashboard
// panel was written against.
type Snapshot struct {
	TestName string
	At       time.Time
	Clients  []ClientSeries
}

// Snapshot builds the cumulative state for the remote-write push.
func (a *Accumulator) Snapshot(testName string, at time.Time, deliveries map[string]Delivery, sentBytes, recvBytes map[string]float64) Snapshot {
	a.mu.Lock()
	clients := make([]*clientAccum, 0, len(a.clients))
	names := make([]string, 0, len(a.clients))
	for name := range a.clients {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		clients = append(clients, a.clients[name].snapshotLocked())
	}
	a.mu.Unlock()

	snap := Snapshot{TestName: testName, At: at, Clients: make([]ClientSeries, 0, len(clients))}
	for i, name := range names {
		client := clients[i]
		delivery := deliveries[name]

		cs := ClientSeries{
			Name:       name,
			ClientType: client.clientType,
			Duration:   summarizeValues(client.iterations),
			QueueDelay: summarizeValues(client.queue),
			Count:      client.overall.count(),
			Iterations: int64(len(client.iterations)),
			Errors:     client.overall.errors(),
			ReqBytes:   sentBytes[name],
			RespBytes:  recvBytes[name],
			Delivery:   delivery,
		}

		keys := make([]seriesKey, 0, len(client.keyed))
		for key := range client.keyed {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].method != keys[j].method {
				return keys[i].method < keys[j].method
			}
			if keys[i].name != keys[j].name {
				return keys[i].name < keys[j].name
			}
			if keys[i].status != keys[j].status {
				return keys[i].status < keys[j].status
			}
			return keys[i].outcome < keys[j].outcome
		})

		type totalsKey struct{ name, method string }
		totals := make(map[totalsKey]*group)

		for _, key := range keys {
			g := client.keyed[key]
			phases := make(map[Phase]types.MetricSummary, len(PhaseNames))
			for _, phase := range PhaseNames {
				phases[phase] = summarizeValues(g.phases[phase])
			}
			cs.Series = append(cs.Series, MethodSeries{
				Name:      key.name,
				Method:    key.method,
				Status:    key.status,
				Outcome:   key.outcome,
				Phases:    phases,
				RespBytes: summarizeValues(g.respBytes),
				Count:     g.count(),
			})

			tk := totalsKey{key.name, key.method}
			merged, ok := totals[tk]
			if !ok {
				merged = newGroup()
				totals[tk] = merged
			}
			merged.merge(g)
		}

		totalKeys := make([]totalsKey, 0, len(totals))
		for tk := range totals {
			totalKeys = append(totalKeys, tk)
		}
		sort.Slice(totalKeys, func(i, j int) bool {
			if totalKeys[i].method != totalKeys[j].method {
				return totalKeys[i].method < totalKeys[j].method
			}
			return totalKeys[i].name < totalKeys[j].name
		})
		for _, tk := range totalKeys {
			g := totals[tk]
			var rpcErrors int64
			for _, n := range g.rpcCodes {
				rpcErrors += n
			}
			cs.Totals = append(cs.Totals, MethodTotals{
				Name:      tk.name,
				Method:    tk.method,
				Count:     g.count(),
				Errors:    g.errors(),
				HTTPFails: g.httpFailures(),
				RPCErrors: rpcErrors,
				RPCCodes:  g.rpcCodes,
			})
		}

		snap.Clients = append(snap.Clients, cs)
	}
	return snap
}

// phaseValues concatenates one phase's observations across every key. It is the
// client-wide distribution, rebuilt on demand rather than retained twice. Only
// one phase is materialised at a time, so the transient cost is a single
// float64 per sample instead of the nine a duplicate group would hold.
func (c *clientAccum) phaseValues(phase Phase) []float64 {
	var n int
	for _, g := range c.keyed {
		n += len(g.phases[phase])
	}
	out := make([]float64, 0, n)
	for _, g := range c.keyed {
		out = append(out, g.phases[phase]...)
	}
	return out
}

func (c *clientAccum) dnsValues() []float64 {
	var n int
	for _, g := range c.keyed {
		n += len(g.dns)
	}
	out := make([]float64, 0, n)
	for _, g := range c.keyed {
		out = append(out, g.dns...)
	}
	return out
}

func (c *clientAccum) snapshotLocked() *clientAccum {
	out := &clientAccum{
		clientType: c.clientType,
		overall:    c.overall.clone(),
		keyed:      make(map[seriesKey]*group, len(c.keyed)),
		queue:      append([]float64(nil), c.queue...),
		iterations: append([]float64(nil), c.iterations...),
	}
	for key, g := range c.keyed {
		out.keyed[key] = g.clone()
	}
	return out
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

func meanOfNonZero(values []float64) float64 {
	var sum float64
	var n int
	for _, v := range values {
		if v > 0 {
			sum += v
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
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
