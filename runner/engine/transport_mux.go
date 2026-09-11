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

// pending is one in-flight request waiting for its answer.
type pending struct {
	ids  []string
	done chan []byte
	once sync.Once
}

func (p *pending) deliver(frame []byte) {
	p.once.Do(func() { p.done <- frame })
}

// mux matches responses to requests over a connection that carries many at once.
type mux struct {
	mu       sync.Mutex
	waiting  map[string]*pending
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
		return nil, m.closedErr()
	}
	for _, id := range ids {
		if _, taken := m.waiting[id]; taken {
			// Two in-flight requests sharing an id cannot both be answered, and
			// silently matching one to the other's response would fabricate a
			// measurement.
			m.releaseLocked(ids)
			return nil, fmt.Errorf("JSON-RPC id %s is already in flight on this connection", id)
		}
		m.waiting[id] = p
	}
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
	if target != nil {
		m.releaseLocked(target.ids)
	}
	m.mu.Unlock()

	if target != nil {
		target.deliver(frame)
	}
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
	if m.closeErr != nil {
		return m.closeErr
	}
	return errConnectionClosed
}

// reset clears the closed state so the connection can be re-established.
func (m *mux) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = false
	m.closeErr = nil
	m.waiting = make(map[string]*pending)
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
