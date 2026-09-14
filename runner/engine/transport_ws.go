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
	// writeMu serialises frame writes: a WebSocket connection supports only one
	// writer at a time, and concurrent writes corrupt the stream rather than
	// failing loudly.
	writeMu sync.Mutex
	closed  bool
}

func newWebSocketConn(url string, headers map[string]string, timeout time.Duration, opts TransportOptions) *wsConn {
	return &wsConn{url: url, headers: headers, timeout: timeout, opts: opts, mux: newMux()}
}

// connect returns the live socket, dialling if this is the first request or if
// the previous connection went away.
func (w *wsConn) connect(ctx context.Context) (*websocket.Conn, *mux, Phases, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil, nil, Phases{}, errConnectionClosed
	}
	if w.socket != nil {
		return w.socket, w.mux, Phases{}, nil
	}

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
			return nil, nil, Phases{}, fmt.Errorf("websocket handshake rejected with %s: %w", resp.Status, err)
		}
		return nil, nil, Phases{}, fmt.Errorf("websocket dial failed: %w", err)
	}
	elapsed := time.Since(start)

	w.mux.reset()
	w.socket = socket
	go w.read(socket, w.mux)

	// The handshake is a real cost, and it is paid by whichever request happened
	// to open the connection rather than spread across the run.
	return socket, w.mux, Phases{Connecting: elapsed}, nil
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
