package engine

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// wsConn carries every request for one target over a single WebSocket, the way
// a client library with a persistent subscription connection does. Responses
// are matched by JSON-RPC id, so the node is free to answer out of order.
type wsConn struct {
	url     string
	headers map[string]string
	timeout time.Duration
	opts    TransportOptions

	mu     sync.Mutex
	socket *websocket.Conn
	mux    *mux
	// dial is the handshake in progress, if any. Requests arriving during one
	// wait on its result instead of queueing on mu, which a context cannot
	// interrupt: against an unreachable node that turned every waiting VU's
	// deadline into a multiple of the handshake timeout.
	dial *wsDial
	// writeMu serialises frame writes: a WebSocket connection supports only one
	// writer at a time, and concurrent writes corrupt the stream rather than
	// failing loudly.
	writeMu sync.Mutex
	closed  bool
}

// wsDial is one handshake several requests are waiting on. Its fields are
// written before done is closed and only read after, so the waiters need no
// lock of their own.
type wsDial struct {
	done    chan struct{}
	socket  *websocket.Conn
	mux     *mux
	elapsed time.Duration
	err     error
}

func newWebSocketConn(url string, headers map[string]string, timeout time.Duration, opts TransportOptions) *wsConn {
	return &wsConn{url: url, headers: headers, timeout: timeout, opts: opts}
}

// connect returns the live socket and the mux belonging to it, dialling if this
// is the first request or if the previous connection went away.
//
// The mux is per socket rather than per target: a reader blocked on a socket
// that has since been replaced must not be able to fail the requests running
// over its replacement.
func (w *wsConn) connect(ctx context.Context) (*websocket.Conn, *mux, Phases, error) {
	for {
		w.mu.Lock()
		if w.closed {
			w.mu.Unlock()
			return nil, nil, Phases{}, errConnectionClosed
		}
		if w.socket != nil {
			socket, m := w.socket, w.mux
			w.mu.Unlock()
			return socket, m, Phases{}, nil
		}
		if inProgress := w.dial; inProgress != nil {
			w.mu.Unlock()
			select {
			case <-inProgress.done:
				if inProgress.err != nil {
					// Sharing the error rather than dialling again: a second
					// handshake to a node that just refused the first one only
					// makes this request wait twice as long to learn the same
					// thing.
					return nil, nil, Phases{}, inProgress.err
				}
				// Around again rather than taking that dial's socket directly,
				// because it may already have failed and been dropped. A waiter
				// that finds it live reports it as reused: the handshake cost
				// belongs to the request that paid it.
				continue
			case <-ctx.Done():
				return nil, nil, Phases{}, ctx.Err()
			}
		}

		current := &wsDial{done: make(chan struct{})}
		w.dial = current
		w.mu.Unlock()

		current.socket, current.elapsed, current.err = w.handshake(ctx)

		w.mu.Lock()
		w.dial = nil
		if current.err == nil {
			if w.closed {
				current.err = errConnectionClosed
			} else {
				current.mux = newMux()
				w.socket = current.socket
				w.mux = current.mux
				go w.read(current.socket, current.mux)
			}
		}
		w.mu.Unlock()
		close(current.done)

		if current.err != nil {
			if current.socket != nil {
				current.socket.Close()
			}
			return nil, nil, Phases{}, current.err
		}

		// The handshake is a real cost, and it is paid by whichever request
		// happened to open the connection rather than spread across the run.
		return current.socket, current.mux, Phases{Connecting: current.elapsed}, nil
	}
}

func (w *wsConn) handshake(ctx context.Context) (*websocket.Conn, time.Duration, error) {
	header := http.Header{}
	for name, value := range w.headers {
		// Content-Type describes a body a handshake does not have.
		if name == "Content-Type" {
			continue
		}
		header.Set(name, value)
	}

	dialer := &websocket.Dialer{
		HandshakeTimeout:  w.timeout,
		EnableCompression: w.opts.AcceptCompression,
	}

	start := time.Now()
	socket, resp, err := dialer.DialContext(ctx, w.url, header)
	if err != nil {
		if resp != nil {
			return nil, 0, fmt.Errorf("websocket handshake rejected with %s: %w", resp.Status, err)
		}
		return nil, 0, fmt.Errorf("websocket dial failed: %w", err)
	}
	return socket, time.Since(start), nil
}

// read pumps frames off the socket until it fails, handing each to the mux.
func (w *wsConn) read(socket *websocket.Conn, m *mux) {
	for {
		_, frame, err := socket.ReadMessage()
		if err != nil {
			m.fail(fmt.Errorf("websocket read failed: %w", err))
			w.dropIfCurrent(socket)
			return
		}
		m.dispatch(frame)
	}
}

// dropIfCurrent forgets a socket that failed, so the next request dials a fresh
// one rather than writing into a dead connection.
func (w *wsConn) dropIfCurrent(socket *websocket.Conn) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.socket == socket {
		w.socket = nil
		w.mux = nil
		socket.Close()
	}
}

func (w *wsConn) do(ctx context.Context, payload []byte) attempt {
	ids := payloadIDs(payload)

	socket, m, setup, err := w.connect(ctx)
	if err != nil {
		return attempt{phases: setup, err: err}
	}

	p, err := m.register(ids)
	if err != nil {
		return attempt{phases: setup, err: err}
	}

	if w.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, w.timeout)
		defer cancel()
	}

	sendStart := time.Now()
	w.writeMu.Lock()
	// Without a deadline a peer that accepts the connection and stops reading
	// blocks this write forever while holding the mutex, which stalls every
	// other request on the connection and outlives the configured timeout.
	writeErr := socket.SetWriteDeadline(writeDeadline(ctx, w.timeout))
	if writeErr == nil {
		writeErr = socket.WriteMessage(websocket.TextMessage, payload)
	}
	w.writeMu.Unlock()
	sent := time.Now()

	if writeErr != nil {
		m.release(ids)
		// Closing the socket is what wakes its reader, and the reader is what
		// fails the requests still waiting on this connection. Without it they
		// would wait out their deadlines on a socket nothing will answer.
		w.dropIfCurrent(socket)
		return attempt{
			phases:    Phases{Connecting: setup.Connecting, Sending: sent.Sub(sendStart)},
			sentBytes: len(payload),
			err:       fmt.Errorf("websocket write failed: %w", writeErr),
		}
	}

	res := m.await(ctx, p, sent)
	res.phases.Connecting = setup.Connecting
	res.phases.Sending = sent.Sub(sendStart)
	res.sentBytes = len(payload)
	res.reused = setup.Connecting == 0
	return res
}

func (w *wsConn) Close() error {
	w.mu.Lock()
	socket := w.socket
	w.socket = nil
	w.mux = nil
	w.closed = true
	w.mu.Unlock()

	if socket == nil {
		return nil
	}
	// A node keeps a WebSocket open until told otherwise, so a run that walked
	// away without closing would leave a subscription slot held.
	_ = socket.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	return socket.Close()
}
