package probe

import (
	"sort"
	"time"

	"github.com/jsonrpc-bench/runner/freshness/ethrpc"
	"github.com/jsonrpc-bench/runner/freshness/schema"
)

// worker polls one probe for one target. It owns its state; only
// expectation refreshes read target fields, under Runner.mu.
type worker struct {
	r     *Runner
	t     *target
	probe string
	res   *schema.ProbeResult
	sig   *schema.NotReadySignature
	exp   ethrpc.Expectation

	seq          int
	firstSeen    map[string]schema.Stamp
	firstSeq     map[string]int
	cands        map[string]ethrpc.Candidate
	stableDigest string
	stableCount  int
	lastMismatch string
	lags         []int64

	runKey   string
	runLast  *schema.Attempt
	runCount int
}

type polled struct {
	scheduled time.Time
	resp      ethrpc.Response
}

func (w *worker) run() {
	defer w.r.wg.Done()
	defer w.r.workerDone(w.t)
	w.firstSeq = map[string]int{}
	w.cands = map[string]ethrpc.Candidate{}

	cfg := w.r.cfg
	fast := time.Duration(cfg.PollIntervalMs) * time.Millisecond
	slow := time.Duration(cfg.LookaheadPollIntervalMs) * time.Millisecond
	promoted, headerCh := w.t.promoted, w.t.headerCh

	interval := slow
	select {
	case <-promoted:
		interval, promoted = fast, nil
	default:
	}

	var deadline <-chan time.Time
	select {
	case <-headerCh:
		headerCh = nil
		var done bool
		if deadline, done = w.onHeader(); done {
			return
		}
	default:
	}

	results := make(chan polled, cfg.MaxInflightPerProbe)
	inflight := 0
	send := func(scheduled time.Time) {
		if inflight >= cfg.MaxInflightPerProbe {
			w.res.SkippedPolls++
			return
		}
		inflight++
		method, params := w.request()
		go func() {
			results <- polled{scheduled: scheduled, resp: w.r.el.Call(w.t.ctx, method, params...)}
		}()
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	send(time.Now())
	for {
		select {
		case <-w.t.ctx.Done():
			if w.res.MatchReceived != nil {
				w.finish(schema.StatusMatched, "run ended before the answer was confirmed stable")
			} else {
				w.finish(schema.StatusAborted, "run ended before an answer was observed")
			}
			return
		case <-promoted:
			promoted = nil
			ticker.Reset(fast)
			w.refreshExpectation()
		case <-headerCh:
			headerCh = nil
			var done bool
			if deadline, done = w.onHeader(); done {
				return
			}
		case <-deadline:
			w.finishAtDeadline()
			return
		case tick := <-ticker.C:
			send(tick)
		case p := <-results:
			inflight--
			if w.handle(p) {
				w.finish(schema.StatusMatched, "")
				return
			}
		}
	}
}

// onHeader applies what the header reveals: not-applicable cases, pending
// candidates that can now be judged, and the deadline.
func (w *worker) onHeader() (<-chan time.Time, bool) {
	w.refreshExpectation()
	h := w.exp.Header
	switch w.probe {
	case schema.ProbeLogsNumber, schema.ProbeLogsHash:
		if h.LogsBloom == [256]byte{} {
			w.finish(schema.StatusNotApplicable, schema.ReasonEmptyLogs)
			return nil, true
		}
	case schema.ProbeTransactionReceipt, schema.ProbeBlockReceipts:
		if len(h.TxHashes) == 0 {
			w.finish(schema.StatusNotApplicable, schema.ReasonEmptyTransactions)
			return nil, true
		}
	}
	if w.reevaluate() {
		w.finish(schema.StatusMatched, "")
		return nil, true
	}
	w.r.mu.Lock()
	dl := time.Unix(0, int64(w.t.rec.Deadline))
	w.r.mu.Unlock()
	return time.After(time.Until(dl)), false
}

func (w *worker) refreshExpectation() {
	w.r.mu.Lock()
	w.exp = w.r.expectationLocked(w.t)
	w.r.mu.Unlock()
}

func (w *worker) request() (string, []any) {
	var hash [32]byte
	if w.exp.Header != nil {
		hash = w.exp.Header.Hash
	}
	return request(w.probe, w.t.number, hash, w.exp.TxHash)
}

// handle records one response and reports whether the probe is done.
func (w *worker) handle(p polled) bool {
	reply := ethrpc.Parse(p.resp)
	class := reply.Class
	weak := false
	if nr, wk := ethrpc.NotReady(w.sig, reply); nr {
		class, weak = schema.ClassNotReady, wk
	}
	a := schema.Attempt{
		Envelope:    w.r.env,
		Probe:       w.probe,
		BlockNumber: w.t.number,
		Seq:         w.seq,
		Scheduled:   w.r.clock.At(p.scheduled),
		Sent:        p.resp.Sent,
		Received:    p.resp.Received,
		Class:       class,
		Bytes:       reply.Bytes,
		HTTPStatus:  reply.HTTPStatus,
		ErrorCode:   reply.Code,
		WeakMatch:   weak,
	}
	switch {
	case reply.Message != "":
		a.Error = truncate(reply.Message, 200)
	case reply.Err != "":
		a.Error = truncate(reply.Err, 200)
	}
	w.seq++
	w.res.Attempts++
	w.res.Classes[class]++
	if w.res.FirstSent.IsZero() {
		w.res.FirstSent = p.resp.Sent
	}
	if !p.resp.SentAt.IsZero() {
		w.lags = append(w.lags, p.resp.SentAt.Sub(p.scheduled).Microseconds())
	}

	if class == schema.ClassResult {
		cand, err := ethrpc.Canonicalize(w.probe, reply.Result)
		if err != nil {
			a.Local, a.LocalReason = schema.LocalMismatch, "unparseable result: "+truncate(err.Error(), 150)
		} else {
			a.Digest = cand.Digest
			w.r.responses.Put(cand.Digest, cand.Canon)
			if _, ok := w.firstSeen[cand.Digest]; !ok {
				w.firstSeen[cand.Digest] = a.Received
				w.firstSeq[cand.Digest] = a.Seq
				w.cands[cand.Digest] = cand
			}
			if !cand.OrderOK {
				w.res.OrderViolation = true
			}
			a.Local, a.LocalReason = ethrpc.Check(w.probe, cand, w.exp)
			if !hashAddressed(w.probe) {
				w.r.blockVisible(w.t)
			}
		}
	}
	w.record(a)
	return w.evaluate(a)
}

func (w *worker) evaluate(a schema.Attempt) bool {
	if a.Local != schema.LocalMatch {
		w.stableDigest, w.stableCount = "", 0
		if a.Local == schema.LocalMismatch {
			w.lastMismatch = a.LocalReason
		}
		return false
	}
	if a.Digest == w.stableDigest {
		w.stableCount++
	} else {
		w.stableDigest, w.stableCount = a.Digest, 1
	}
	w.markMatch(a.Digest)
	need := 1
	if needsStablePolls(w.probe) {
		need = w.r.cfg.LogsStablePolls
	}
	return w.stableCount >= need
}

func (w *worker) markMatch(digest string) {
	if w.res.MatchDigest == digest {
		return
	}
	first := w.firstSeen[digest]
	w.res.MatchReceived = &first
	w.res.MatchDigest = digest
	w.res.LeftCensored = w.firstSeq[digest] == 0
}

// reevaluate judges candidates that arrived before the header was known.
// Only probes that need a single match can finish here; the others still
// need stable polls after the header.
func (w *worker) reevaluate() bool {
	digests := make([]string, 0, len(w.cands))
	for d := range w.cands {
		digests = append(digests, d)
	}
	sort.Slice(digests, func(i, j int) bool { return w.firstSeq[digests[i]] < w.firstSeq[digests[j]] })
	for _, d := range digests {
		if v, _ := ethrpc.Check(w.probe, w.cands[d], w.exp); v == schema.LocalMatch {
			w.markMatch(d)
			return !needsStablePolls(w.probe)
		}
	}
	return false
}

func (w *worker) finishAtDeadline() {
	switch {
	case w.res.MatchReceived != nil:
		w.finish(schema.StatusMatched, "deadline reached before the answer was confirmed stable")
	case w.lastMismatch != "":
		w.finish(schema.StatusDeadlineExceeded, "last local check: "+w.lastMismatch)
	default:
		w.finish(schema.StatusDeadlineExceeded, "")
	}
}

func (w *worker) finish(status, reason string) {
	w.flushRun()
	w.res.Status, w.res.Reason = status, reason
	if len(w.lags) > 0 {
		sort.Slice(w.lags, func(i, j int) bool { return w.lags[i] < w.lags[j] })
		w.res.LagP50Micros = w.lags[len(w.lags)/2]
		w.res.LagMaxMicros = w.lags[len(w.lags)-1]
	}
}

// record writes an attempt when its outcome differs from the previous one,
// plus the last attempt of the run it ends. The last "not ready" before a
// correct answer therefore always survives: it is the availability lower bound.
func (w *worker) record(a schema.Attempt) {
	if w.r.opts.RecordAllAttempts {
		a.Edge = schema.EdgeAll
		w.r.attempts.Write(a)
		return
	}
	key := a.Class + "|" + a.Digest + "|" + a.LocalReason
	if a.Class != schema.ClassResult {
		key += "|" + ethrpc.NormalizeMessage(a.Error)
	}
	if key == w.runKey {
		w.runLast = &a
		w.runCount++
		return
	}
	w.flushRun()
	a.Edge = schema.EdgeFirst
	w.r.attempts.Write(a)
	w.runKey, w.runCount, w.runLast = key, 1, nil
}

func (w *worker) flushRun() {
	if w.runLast == nil {
		return
	}
	last := *w.runLast
	last.Edge = schema.EdgeLast
	last.Repeat = w.runCount
	w.r.attempts.Write(last)
	w.runLast = nil
}
