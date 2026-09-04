// Package stubnode serves a JSON-RPC endpoint whose latency and failures are
// configured rather than real, so load-engine behaviour can be tested against a
// known distribution.
//
// Every response is derived from the request's JSON-RPC id, not from a random
// source: two runs of the same request sequence see identical service times and
// identical injected failures, in any arrival order and at any concurrency.
// That is what makes an A/B comparison between two load engines reproducible
// instead of statistical, and what removes the stub as a source of variance
// when several clients replay one sequence.
package stubnode

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	LatencyFixed     = "fixed"
	LatencyUniform   = "uniform"
	LatencyLogNormal = "lognormal"
)

// Latency describes the service time distribution for a method.
type Latency struct {
	Kind  string  `json:"kind"`
	MS    float64 `json:"ms"`
	MinMS float64 `json:"min_ms"`
	MaxMS float64 `json:"max_ms"`
	P50MS float64 `json:"p50_ms"`
	Sigma float64 `json:"sigma"`
}

// Method overrides the default behaviour for one RPC method.
type Method struct {
	Latency         *Latency `json:"latency,omitempty"`
	RPCErrorRate    float64  `json:"rpc_error_rate,omitempty"`
	RPCErrorCode    int      `json:"rpc_error_code,omitempty"`
	RPCErrorMessage string   `json:"rpc_error_message,omitempty"`
	NullResultRate  float64  `json:"null_result_rate,omitempty"`
	ResultBytes     int      `json:"result_bytes,omitempty"`
}

// Faults are injected regardless of method.
type Faults struct {
	HTTP500Rate  float64 `json:"http_500_rate,omitempty"`
	HTTP429Rate  float64 `json:"http_429_rate,omitempty"`
	TruncateRate float64 `json:"truncate_rate,omitempty"`
	TimeoutRate  float64 `json:"timeout_rate,omitempty"`
	TimeoutMS    float64 `json:"timeout_ms,omitempty"`
}

// Node is what the stub answers the identity probes with, so a pre-flight
// check has something realistic to read.
type Node struct {
	ClientVersion string `json:"client_version,omitempty"`
	ChainID       int64  `json:"chain_id,omitempty"`
	HeadBlock     int64  `json:"head_block,omitempty"`

	// HeadAgeSeconds ages the head block's timestamp, which is how a stale or
	// stalled node is modelled.
	HeadAgeSeconds int64 `json:"head_age_seconds,omitempty"`

	Syncing bool `json:"syncing,omitempty"`

	// ProbeError makes the identity probes fail, which models a node that is
	// up but not answerable. The general fault rates deliberately do not apply
	// to those methods, so an unrelated fault rate cannot make a pre-flight
	// check flaky.
	ProbeError bool `json:"probe_error,omitempty"`

	// MaxBatchSize refuses a batch larger than this with a single error object
	// rather than an array, which is how a node with a batch limit answers.
	// Zero accepts any size.
	MaxBatchSize int `json:"max_batch_size,omitempty"`

	// Metrics makes the stub publish a Prometheus endpoint at /metrics, so a
	// run can be pointed at it the way it would be pointed at a real node's.
	// CPUSecondsPerRequest and BytesPerRequest make the published figures move
	// with the load, which is what makes a correlation worth checking.
	Metrics              bool    `json:"metrics,omitempty"`
	CPUSecondsPerRequest float64 `json:"cpu_seconds_per_request,omitempty"`
	BytesPerRequest      float64 `json:"bytes_per_request,omitempty"`

	// ConcurrencyLimit is how many requests the node serves at once, as
	// Nethermind's JsonRpc.EthModuleConcurrentInstances does. Beyond it
	// requests queue, so latency degrades with offered load and the node has a
	// real capacity of roughly ConcurrencyLimit/serviceTime requests a second.
	//
	// Queueing makes total latency depend on arrival timing, so it is not
	// reproducible the way service time and fault injection are. Leave it unset
	// for an A/B comparison; set it to measure saturation behaviour.
	ConcurrencyLimit int `json:"concurrency_limit,omitempty"`
}

// Config is the whole stub definition, loadable from JSON.
type Config struct {
	Seed    int64             `json:"seed"`
	Node    Node              `json:"node,omitempty"`
	Default Method            `json:"default"`
	Methods map[string]Method `json:"methods,omitempty"`
	Faults  Faults            `json:"faults,omitempty"`
}

// Outcome names what the stub did with a request, matching the classes a load
// engine is expected to distinguish.
type Outcome string

const (
	OutcomeOK        Outcome = "ok"
	OutcomeRPCError  Outcome = "rpc_error"
	OutcomeRPCNull   Outcome = "rpc_null"
	OutcomeHTTPError Outcome = "http_error"
	OutcomeTruncated Outcome = "truncated"
	OutcomeTimeout   Outcome = "timeout"
	OutcomeBadInput  Outcome = "bad_input"
)

// Stats is the tally the stub kept, used to assert what an engine reported
// against what was actually served.
type Stats struct {
	Total    int64                        `json:"total"`
	ByMethod map[string]map[Outcome]int64 `json:"by_method"`
}

type Stub struct {
	cfg     Config
	pad     []byte
	workers chan struct{}

	mu    sync.Mutex
	total int64
	seen  map[string]map[Outcome]int64
}

// DefaultConfig is a fast, failure-free node.
func DefaultConfig() Config {
	return Config{
		Seed: 1,
		Node: Node{
			ClientVersion: "stubnode/v1.0.0",
			ChainID:       1,
			HeadBlock:     21000000,
		},
		Default: Method{Latency: &Latency{Kind: LatencyUniform, MinMS: 2, MaxMS: 8}},
	}
}

func New(cfg Config) (*Stub, error) {
	if cfg.Default.Latency == nil {
		cfg.Default.Latency = DefaultConfig().Default.Latency
	}
	defaults := DefaultConfig().Node
	if cfg.Node.ClientVersion == "" {
		cfg.Node.ClientVersion = defaults.ClientVersion
	}
	if cfg.Node.ChainID == 0 {
		cfg.Node.ChainID = defaults.ChainID
	}
	if cfg.Node.HeadBlock == 0 {
		cfg.Node.HeadBlock = defaults.HeadBlock
	}
	if err := validate(cfg); err != nil {
		return nil, err
	}

	maxBytes := cfg.Default.ResultBytes
	for _, m := range cfg.Methods {
		if m.ResultBytes > maxBytes {
			maxBytes = m.ResultBytes
		}
	}
	pad := make([]byte, maxBytes)
	for i := range pad {
		pad[i] = "0123456789abcdef"[i%16]
	}

	stub := &Stub{cfg: cfg, pad: pad, seen: make(map[string]map[Outcome]int64)}
	if cfg.Node.ConcurrencyLimit > 0 {
		stub.workers = make(chan struct{}, cfg.Node.ConcurrencyLimit)
	}
	return stub, nil
}

func validate(cfg Config) error {
	check := func(name string, m Method) error {
		if m.Latency != nil {
			switch m.Latency.Kind {
			case LatencyFixed, LatencyUniform, LatencyLogNormal, "":
			default:
				return fmt.Errorf("%s: unknown latency kind %q", name, m.Latency.Kind)
			}
			if m.Latency.Kind == LatencyUniform && m.Latency.MaxMS < m.Latency.MinMS {
				return fmt.Errorf("%s: max_ms is below min_ms", name)
			}
		}
		for label, rate := range map[string]float64{
			"rpc_error_rate":   m.RPCErrorRate,
			"null_result_rate": m.NullResultRate,
		} {
			if rate < 0 || rate > 1 {
				return fmt.Errorf("%s: %s must be within 0..1", name, label)
			}
		}
		return nil
	}
	if err := check("default", cfg.Default); err != nil {
		return err
	}
	for name, m := range cfg.Methods {
		if err := check(name, m); err != nil {
			return err
		}
	}
	if total := cfg.Faults.HTTP500Rate + cfg.Faults.HTTP429Rate + cfg.Faults.TruncateRate + cfg.Faults.TimeoutRate; total > 1 {
		return fmt.Errorf("fault rates sum to %.2f, which exceeds 1", total)
	}
	return nil
}

func (s *Stub) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Stats{Total: s.total, ByMethod: make(map[string]map[Outcome]int64, len(s.seen))}
	for method, outcomes := range s.seen {
		copied := make(map[Outcome]int64, len(outcomes))
		for o, n := range outcomes {
			copied[o] = n
		}
		out.ByMethod[method] = copied
	}
	return out
}

func (s *Stub) record(method string, outcome Outcome) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.total++
	if s.seen[method] == nil {
		s.seen[method] = make(map[Outcome]int64, 4)
	}
	s.seen[method][outcome]++
}

// Handler serves JSON-RPC on POST / and the tally on GET /__stats.
func (s *Stub) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/__stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(s.Stats())
	})
	if s.cfg.Node.Metrics {
		mux.HandleFunc("/metrics", s.serveMetrics)
	}
	mux.HandleFunc("/", s.serveRPC)
	return mux
}

// serveMetrics publishes the shape a node's own metrics endpoint has, with
// figures that move with the load so a correlation can be checked rather than
// merely plumbed.
func (s *Stub) serveMetrics(w http.ResponseWriter, r *http.Request) {
	stats := s.Stats()

	cpu := float64(stats.Total) * s.cfg.Node.CPUSecondsPerRequest
	resident := 128 << 20
	if s.cfg.Node.BytesPerRequest > 0 {
		resident += int(float64(stats.Total) * s.cfg.Node.BytesPerRequest)
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, `# HELP process_cpu_seconds_total Total user and system CPU time spent in seconds.
# TYPE process_cpu_seconds_total counter
process_cpu_seconds_total %.6f
# HELP process_resident_memory_bytes Resident memory size in bytes.
# TYPE process_resident_memory_bytes gauge
process_resident_memory_bytes %d
# HELP dotnet_collection_count_total GC collection count.
# TYPE dotnet_collection_count_total counter
dotnet_collection_count_total{generation="0"} %d
dotnet_collection_count_total{generation="1"} %d
# HELP stub_requests_total Requests the stub has served.
# TYPE stub_requests_total counter
stub_requests_total %d
# HELP stub_head_block The head block the stub reports.
# TYPE stub_head_block gauge
stub_head_block %d
`, cpu, resident, stats.Total/10, stats.Total/100, stats.Total, s.cfg.Node.HeadBlock)
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      json.RawMessage `json:"id"`
}

func (s *Stub) serveRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "expected POST", http.StatusMethodNotAllowed)
		return
	}

	body, err := readAll(r)
	if err != nil {
		s.record("", OutcomeBadInput)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if firstNonSpace(body) == '[' {
		s.serveBatch(w, r, body)
		return
	}

	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil || req.Method == "" {
		s.record("", OutcomeBadInput)
		writeJSON(w, http.StatusOK, map[string]any{
			"jsonrpc": "2.0", "id": nil,
			"error": map[string]any{"code": -32700, "message": "stubnode: parse error"},
		})
		return
	}

	// Identity probes answer immediately and are exempt from the configured
	// latency and faults: a pre-flight check is not part of the measured load.
	if result, ok := s.identity(req.Method); ok {
		if s.cfg.Node.ProbeError {
			s.record(req.Method, OutcomeHTTPError)
			http.Error(w, "node is not answerable", http.StatusInternalServerError)
			return
		}
		s.record(req.Method, OutcomeOK)
		writeJSON(w, http.StatusOK, map[string]any{
			"jsonrpc": "2.0", "id": rawOrNull(req.ID), "result": result,
		})
		return
	}

	key := requestKey(req.ID, body)
	method := s.methodConfig(req.Method)

	if !s.acquireWorker(r) {
		s.record(req.Method, OutcomeTimeout)
		return
	}
	defer s.releaseWorker()

	if !s.sleep(r, method.Latency, req.Method, key) {
		s.record(req.Method, OutcomeTimeout)
		return
	}

	switch outcome := s.fault(req.Method, key); outcome {
	case OutcomeHTTPError:
		s.serveHTTPFault(w, req.Method, key)
		return
	case OutcomeTruncated:
		s.record(req.Method, OutcomeTruncated)
		serveTruncated(w, req.ID)
		return
	case OutcomeTimeout:
		s.hang(r, req.Method)
		return
	}

	if s.draw(req.Method, key, "rpcerr") < method.RPCErrorRate {
		code := method.RPCErrorCode
		if code == 0 {
			code = -32000
		}
		message := method.RPCErrorMessage
		if message == "" {
			message = "execution reverted"
		}
		s.record(req.Method, OutcomeRPCError)
		writeJSON(w, http.StatusOK, map[string]any{
			"jsonrpc": "2.0", "id": rawOrNull(req.ID),
			"error": map[string]any{"code": code, "message": message},
		})
		return
	}

	if s.draw(req.Method, key, "null") < method.NullResultRate {
		s.record(req.Method, OutcomeRPCNull)
		writeJSON(w, http.StatusOK, map[string]any{
			"jsonrpc": "2.0", "id": rawOrNull(req.ID), "result": nil,
		})
		return
	}

	s.record(req.Method, OutcomeOK)
	writeJSON(w, http.StatusOK, map[string]any{
		"jsonrpc": "2.0", "id": rawOrNull(req.ID), "result": s.resultFor(req.Method, method),
	})
}

// resultFor answers with a shape the method's callers expect. Only
// eth_getBlockByNumber needs one: a pre-flight check reads its timestamp to
// tell whether the head is still moving.
func (s *Stub) resultFor(method string, cfg Method) any {
	if method == "eth_getBlockByNumber" {
		return map[string]any{
			"number":    hexUint(s.cfg.Node.HeadBlock),
			"hash":      "0x" + strings.Repeat("ab", 32),
			"timestamp": hexUint(time.Now().Unix() - s.cfg.Node.HeadAgeSeconds),
		}
	}
	return s.result(cfg.ResultBytes)
}

func (s *Stub) serveHTTPFault(w http.ResponseWriter, method string, key uint64) {
	s.record(method, OutcomeHTTPError)
	total := s.cfg.Faults.HTTP500Rate + s.cfg.Faults.HTTP429Rate
	if total > 0 && s.draw(method, key, "faultkind")*total < s.cfg.Faults.HTTP429Rate {
		w.Header().Set("Retry-After", "1")
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// hang holds the request open past any sane client deadline so the engine has
// to classify it as a timeout, releasing the goroutine as soon as the client
// gives up.
func (s *Stub) hang(r *http.Request, method string) {
	d := time.Duration(s.cfg.Faults.TimeoutMS) * time.Millisecond
	if d <= 0 {
		d = time.Minute
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-r.Context().Done():
	}
	s.record(method, OutcomeTimeout)
}

func (s *Stub) sleep(r *http.Request, l *Latency, method string, key uint64) bool {
	d := s.latency(l, method, key)
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-r.Context().Done():
		return false
	}
}

func (s *Stub) latency(l *Latency, method string, key uint64) time.Duration {
	if l == nil {
		return 0
	}
	var ms float64
	switch l.Kind {
	case LatencyFixed:
		ms = l.MS
	case LatencyUniform:
		ms = l.MinMS + s.draw(method, key, "latency")*(l.MaxMS-l.MinMS)
	case LatencyLogNormal:
		sigma := l.Sigma
		if sigma <= 0 {
			sigma = 0.5
		}
		ms = l.P50MS * math.Exp(sigma*s.normal(method, key))
	}
	if ms <= 0 {
		return 0
	}
	return time.Duration(ms * float64(time.Millisecond))
}

func (s *Stub) fault(method string, key uint64) Outcome {
	f := s.cfg.Faults
	r := s.draw(method, key, "fault")
	switch {
	case r < f.HTTP500Rate+f.HTTP429Rate:
		return OutcomeHTTPError
	case r < f.HTTP500Rate+f.HTTP429Rate+f.TruncateRate:
		return OutcomeTruncated
	case r < f.HTTP500Rate+f.HTTP429Rate+f.TruncateRate+f.TimeoutRate:
		return OutcomeTimeout
	}
	return OutcomeOK
}

func (s *Stub) methodConfig(method string) Method {
	if m, ok := s.cfg.Methods[method]; ok {
		if m.Latency == nil {
			m.Latency = s.cfg.Default.Latency
		}
		return m
	}
	return s.cfg.Default
}

func (s *Stub) result(resultBytes int) string {
	if resultBytes <= 0 {
		return "0x1"
	}
	return "0x" + string(s.pad[:resultBytes])
}

// draw returns a value in [0,1) fixed by the seed, method, request key and
// stream name, so each decision has its own reproducible sequence.
func (s *Stub) draw(method string, key uint64, stream string) float64 {
	h := fnv.New64a()
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(s.cfg.Seed))
	_, _ = h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], key)
	_, _ = h.Write(buf[:])
	_, _ = h.Write([]byte(method))
	_, _ = h.Write([]byte(stream))
	return float64(h.Sum64()>>11) / float64(uint64(1)<<53)
}

func (s *Stub) normal(method string, key uint64) float64 {
	u1 := s.draw(method, key, "normal1")
	u2 := s.draw(method, key, "normal2")
	if u1 <= 0 {
		u1 = math.SmallestNonzeroFloat64
	}
	return math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)
}

// requestKey identifies a request for the purposes of determinism. The runner
// numbers generated requests sequentially, so the id is stable across clients
// and across runs of the same sequence; a request without a usable numeric id
// falls back to its own bytes.
func requestKey(id json.RawMessage, body []byte) uint64 {
	if n, err := strconv.ParseUint(string(id), 10, 64); err == nil {
		return n
	}
	h := fnv.New64a()
	_, _ = h.Write(body)
	return h.Sum64()
}

func rawOrNull(id json.RawMessage) any {
	if len(id) == 0 {
		return nil
	}
	return id
}

func firstNonSpace(b []byte) byte {
	for _, c := range b {
		switch c {
		case ' ', '\t', '\r', '\n':
			continue
		default:
			return c
		}
	}
	return 0
}

// identity answers the methods a pre-flight check uses to establish what a node
// is. The second return reports whether the method was one of them.
func (s *Stub) identity(method string) (any, bool) {
	switch method {
	case "web3_clientVersion":
		return s.cfg.Node.ClientVersion, true
	case "eth_chainId":
		return hexUint(s.cfg.Node.ChainID), true
	case "eth_blockNumber":
		return hexUint(s.cfg.Node.HeadBlock), true
	case "eth_syncing":
		if !s.cfg.Node.Syncing {
			return false, true
		}
		return map[string]any{
			"startingBlock": hexUint(0),
			"currentBlock":  hexUint(s.cfg.Node.HeadBlock),
			"highestBlock":  hexUint(s.cfg.Node.HeadBlock + 1000),
		}, true
	}
	return nil, false
}

func hexUint(v int64) string {
	return "0x" + strconv.FormatInt(v, 16)
}

// acquireWorker waits for one of the node's serving slots. Requests beyond the
// limit queue here, which is where a saturated node's latency comes from.
func (s *Stub) acquireWorker(r *http.Request) bool {
	if s.workers == nil {
		return true
	}
	select {
	case s.workers <- struct{}{}:
		return true
	case <-r.Context().Done():
		return false
	}
}

func (s *Stub) releaseWorker() {
	if s.workers != nil {
		<-s.workers
	}
}

// serveBatch answers a JSON-RPC array. Each member is resolved independently,
// so a batch can come back partly successful — which is the case a client-side
// error rate has to attribute correctly.
//
// The responses are returned in reverse order on purpose: the spec does not
// promise an order, and a client that matches by position rather than by id
// should fail against this stub rather than in production.
func (s *Stub) serveBatch(w http.ResponseWriter, r *http.Request, body []byte) {
	var requests []rpcRequest
	if err := json.Unmarshal(body, &requests); err != nil {
		s.record("batch", OutcomeBadInput)
		writeJSON(w, http.StatusOK, map[string]any{
			"jsonrpc": "2.0", "id": nil,
			"error": map[string]any{"code": -32700, "message": "stubnode: parse error"},
		})
		return
	}

	if len(requests) == 0 {
		s.record("batch", OutcomeBadInput)
		writeJSON(w, http.StatusOK, map[string]any{
			"jsonrpc": "2.0", "id": nil,
			"error": map[string]any{"code": -32600, "message": "stubnode: empty batch"},
		})
		return
	}

	// A node with a batch limit rejects the whole array with one error object,
	// not with an array of them.
	if s.cfg.Node.MaxBatchSize > 0 && len(requests) > s.cfg.Node.MaxBatchSize {
		s.record("batch", OutcomeBadInput)
		writeJSON(w, http.StatusOK, map[string]any{
			"jsonrpc": "2.0", "id": nil,
			"error": map[string]any{
				"code":    -32600,
				"message": fmt.Sprintf("stubnode: batch of %d exceeds the limit of %d", len(requests), s.cfg.Node.MaxBatchSize),
			},
		})
		return
	}

	if !s.acquireWorker(r) {
		s.record("batch", OutcomeTimeout)
		return
	}
	defer s.releaseWorker()

	// One service time for the whole round trip, taken from the slowest member,
	// which is what a node processing a batch actually costs its caller.
	var slowest time.Duration
	responses := make([]map[string]any, 0, len(requests))
	for _, req := range requests {
		method := s.methodConfig(req.Method)
		key := requestKey(req.ID, body)
		if d := s.latency(method.Latency, req.Method, key); d > slowest {
			slowest = d
		}
		responses = append(responses, s.batchMember(req, method, key))
	}

	if slowest > 0 {
		timer := time.NewTimer(slowest)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-r.Context().Done():
			return
		}
	}

	for i, j := 0, len(responses)-1; i < j; i, j = i+1, j-1 {
		responses[i], responses[j] = responses[j], responses[i]
	}
	writeJSON(w, http.StatusOK, responses)
}

// batchMember resolves one call inside a batch and records its outcome.
func (s *Stub) batchMember(req rpcRequest, method Method, key uint64) map[string]any {
	if result, ok := s.identity(req.Method); ok {
		s.record(req.Method, OutcomeOK)
		return map[string]any{"jsonrpc": "2.0", "id": rawOrNull(req.ID), "result": result}
	}

	if s.draw(req.Method, key, "rpcerr") < method.RPCErrorRate {
		code := method.RPCErrorCode
		if code == 0 {
			code = -32000
		}
		message := method.RPCErrorMessage
		if message == "" {
			message = "execution reverted"
		}
		s.record(req.Method, OutcomeRPCError)
		return map[string]any{
			"jsonrpc": "2.0", "id": rawOrNull(req.ID),
			"error": map[string]any{"code": code, "message": message},
		}
	}

	if s.draw(req.Method, key, "null") < method.NullResultRate {
		s.record(req.Method, OutcomeRPCNull)
		return map[string]any{"jsonrpc": "2.0", "id": rawOrNull(req.ID), "result": nil}
	}

	s.record(req.Method, OutcomeOK)
	return map[string]any{
		"jsonrpc": "2.0", "id": rawOrNull(req.ID), "result": s.resultFor(req.Method, method),
	}
}
