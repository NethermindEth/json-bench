package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// errConnectionClosed is what a pending request sees when the node hangs up
// before answering it.
var errConnectionClosed = errors.New("the connection closed before the node answered")

// payloadIDs lists the JSON-RPC ids a payload is asking about: one for a plain
// call, several for a batch. Matching on these rather than on arrival order is
// what makes a multiplexed transport safe — a node may answer in any order, and
// over one socket every answer arrives on the same wire.
func payloadIDs(payload []byte) []string {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return nil
	}
	if trimmed[0] == '[' {
		var members []json.RawMessage
		if err := json.Unmarshal(trimmed, &members); err != nil {
			return nil
		}
		out := make([]string, 0, len(members))
		for _, member := range members {
			if id := envelopeID(member); id != "" {
				out = append(out, id)
			}
		}
		return out
	}
	if id := envelopeID(trimmed); id != "" {
		return []string{id}
	}
	return nil
}

func envelopeID(raw json.RawMessage) string {
	var envelope struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return ""
	}
	id := string(bytes.TrimSpace(envelope.ID))
	if id == "" || id == "null" {
		return ""
	}
	return id
}

// JSON-RPC's two "the request could not be read" codes. The spec says an id
// MUST be null only when the server failed to detect it — a parse error or an
// invalid request — so these are the codes a frame with no id can legitimately
// be answering a request with. A node refusing a batch over its own limit
// answers -32600.
const (
	codeInvalidRequest = -32600
	codeParseError     = -32700
)

// refusesTheRequestItself reports whether a frame is one of those two: an error
// the node raised about the shape of what it was sent, rather than about what
// the call asked for.
//
// It is deliberately narrow. Any other error carrying no id is not a response
// to a request at all — geth and erigon push connection-level errors down the
// socket that way — and handing one of those to a waiting request would fail a
// call the node never refused.
func refusesTheRequestItself(payload []byte) bool {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return false
	}
	if trimmed[0] == '[' {
		var members []json.RawMessage
		if err := json.Unmarshal(trimmed, &members); err != nil {
			return false
		}
		for _, member := range members {
			if refusesTheRequestItself(member) {
				return true
			}
		}
		return false
	}

	var envelope struct {
		Error *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(trimmed, &envelope); err != nil || envelope.Error == nil {
		return false
	}
	return envelope.Error.Code == codeInvalidRequest || envelope.Error.Code == codeParseError
}

// writeDeadline is when a send must have completed by: the request's own
// deadline when it has one, the transport timeout otherwise.
func writeDeadline(ctx context.Context, timeout time.Duration) time.Time {
	if deadline, ok := ctx.Deadline(); ok {
		return deadline
	}
	if timeout <= 0 {
		return time.Time{}
	}
	return time.Now().Add(timeout)
}

// pending is one in-flight request waiting for its answer.
type pending struct {
	ids  []string
	seq  uint64
	done chan []byte
	once sync.Once
}

func (p *pending) deliver(frame []byte) {
	p.once.Do(func() { p.done <- frame })
}

// mux matches responses to requests over a connection that carries many at
// once. One mux belongs to one socket: a reconnect gets a fresh one, so a
// reader still blocked on the socket it was given cannot fail requests that
// now belong to the connection replacing it.
type mux struct {
	mu       sync.Mutex
	waiting  map[string]*pending
	nextSeq  uint64
	closed   bool
	closeErr error
}

func newMux() *mux {
	return &mux{waiting: make(map[string]*pending)}
}

// register claims every id the payload asks about. A batch registers under all
// of them because the node may answer with the members in any order, so the
// first id to come back is whichever it chose to put first.
func (m *mux) register(ids []string) (*pending, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("the request carries no JSON-RPC id, so its answer could not be matched")
	}

	p := &pending{ids: ids, done: make(chan []byte, 1)}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, m.closedErrLocked()
	}
	for i, id := range ids {
		if _, taken := m.waiting[id]; taken {
			// Two in-flight requests sharing an id cannot both be answered, and
			// silently matching one to the other's response would fabricate a
			// measurement. Only the ids claimed above are released: the one that
			// collided belongs to the other request, and dropping it here would
			// orphan that request into a timeout it never had.
			m.releaseLocked(ids[:i])
			return nil, fmt.Errorf("JSON-RPC id %s is already in flight on this connection", id)
		}
		m.waiting[id] = p
	}
	m.nextSeq++
	p.seq = m.nextSeq
	return p, nil
}

func (m *mux) release(ids []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.releaseLocked(ids)
}

func (m *mux) releaseLocked(ids []string) {
	for _, id := range ids {
		delete(m.waiting, id)
	}
}

// dispatch hands a frame to whoever asked for it. A frame nobody is waiting on
// is dropped: a node may push subscription notifications down the same socket,
// and those are not answers to anything measured here.
func (m *mux) dispatch(frame []byte) {
	ids := payloadIDs(frame)
	m.mu.Lock()
	var target *pending
	for _, id := range ids {
		if p, ok := m.waiting[id]; ok {
			target = p
			break
		}
	}
	if target == nil && len(ids) == 0 && refusesTheRequestItself(frame) {
		// A node that could not read the id it should answer with — a parse
		// error, or a batch over its own limit — answers "id": null. There is
		// nothing to match on, so dropping the frame would report the node's
		// answer as a timeout, which is the one thing it certainly was not.
		target = m.refusalTargetLocked()
	}
	if target != nil {
		m.releaseLocked(target.ids)
	}
	m.mu.Unlock()

	if target != nil {
		target.deliver(frame)
	}
}

// refusalTargetLocked picks the request an unattributable refusal belongs to,
// or nil when that cannot be told.
//
// It is the request waiting longest, but only while every request in flight
// carries the same number of calls. A node refusing on the shape of what it was
// sent refuses them all alike, so each gets a frame of its own and both the
// count and the call it lands on come out right.
//
// Once the shapes differ the frame is indistinguishable from one belonging to a
// request the node did answer, and guessing is worse than dropping it: the
// refusal would be recorded against a call that succeeded, that call's real
// answer would arrive to find its ids released and be discarded, and the call
// the node actually refused would still time out. One refusal would become two
// failures and a lost success.
//
// Refusing to guess unless exactly one request is in flight would close that
// too, but it would also undo the fix it exists for: the first moment two
// requests overlap, both refusals are dropped, and the timeouts they turn into
// hold the in-flight slots open so that nothing is ever the sole waiter again.
// One transient overlap would poison the rest of the run.
func (m *mux) refusalTargetLocked() *pending {
	calls := -1
	var oldest *pending
	for _, p := range m.waiting {
		// A batch is registered under every id it carries, so the count of
		// claimed ids is the number of calls in flight for that request.
		if calls < 0 {
			calls = len(p.ids)
		} else if len(p.ids) != calls {
			return nil
		}
		if oldest == nil || p.seq < oldest.seq {
			oldest = p
		}
	}
	return oldest
}

// fail wakes every pending request, which is what a closed connection means for
// all of them.
func (m *mux) fail(err error) {
	m.mu.Lock()
	m.closed = true
	m.closeErr = err
	waiting := make([]*pending, 0, len(m.waiting))
	seen := map[*pending]struct{}{}
	for _, p := range m.waiting {
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		waiting = append(waiting, p)
	}
	m.waiting = make(map[string]*pending)
	m.mu.Unlock()

	for _, p := range waiting {
		p.deliver(nil)
	}
}

func (m *mux) closedErr() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closedErrLocked()
}

func (m *mux) closedErrLocked() error {
	if m.closeErr != nil {
		return m.closeErr
	}
	return errConnectionClosed
}

// await blocks for this request's answer, the deadline, or the connection
// going away underneath it.
func (m *mux) await(ctx context.Context, p *pending, sent time.Time) attempt {
	select {
	case frame := <-p.done:
		if frame == nil {
			return attempt{
				phases: Phases{Sending: 0, Waiting: time.Since(sent)},
				err:    m.closedErr(),
			}
		}
		// A multiplexed reader sees a frame arrive whole, so there is no point
		// between the first byte and the last to split on: the round trip is
		// reported as waiting, and receiving stays zero rather than invented.
		return attempt{
			status: statusMultiplexedOK,
			body:   frame,
			phases: Phases{Waiting: time.Since(sent)},
			reused: true,
		}
	case <-ctx.Done():
		m.release(p.ids)
		return attempt{phases: Phases{Waiting: time.Since(sent)}, err: ctx.Err()}
	}
}

// statusMultiplexedOK stands in for the HTTP status a socket transport does not
// have. The outcome classification keys on 200 meaning "the node answered, now
// read what it said", which is exactly what a delivered frame means here; the
// manifest records the transport so a reader knows the status is synthetic.
const statusMultiplexedOK = 200
