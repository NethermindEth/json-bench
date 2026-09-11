package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// ipcConn carries every request for one target over a Unix domain socket.
//
// IPC is how a node is reached from the machine it runs on, with no TCP stack
// and no HTTP framing in the way, which makes it the transport that shows a
// node's own service time most directly.
type ipcConn struct {
	path    string
	timeout time.Duration

	mu      sync.Mutex
	socket  net.Conn
	mux     *mux
	writeMu sync.Mutex
	closed  bool
}

func newIPCConn(path string, timeout time.Duration) *ipcConn {
	return &ipcConn{path: path, timeout: timeout, mux: newMux()}
}

// maxUnixPath is the smallest sun_path any supported platform offers: macOS
// allows 104 bytes and Linux 108, both including the terminator.
const maxUnixPath = 103

// checkIPCPath catches a socket path the kernel cannot hold. Over the limit the
// syscall fails with "invalid argument", which reads like a bad socket rather
// than a path that is merely too long.
func checkIPCPath(path string) error {
	if len(path) > maxUnixPath {
		return fmt.Errorf("the IPC socket path is %d bytes and the limit is %d: %s",
			len(path), maxUnixPath, path)
	}
	return nil
}

func (c *ipcConn) connect(ctx context.Context) (net.Conn, *mux, Phases, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil, nil, Phases{}, errConnectionClosed
	}
	if c.socket != nil {
		return c.socket, c.mux, Phases{}, nil
	}

	if err := checkIPCPath(c.path); err != nil {
		return nil, nil, Phases{}, err
	}

	dialer := &net.Dialer{Timeout: c.timeout}
	start := time.Now()
	socket, err := dialer.DialContext(ctx, "unix", c.path)
	if err != nil {
		return nil, nil, Phases{}, fmt.Errorf("failed to connect to the IPC socket %s: %w", c.path, err)
	}
	elapsed := time.Since(start)

	c.mux.reset()
	c.socket = socket
	go c.read(socket, c.mux)

	return socket, c.mux, Phases{Connecting: elapsed}, nil
}

// read decodes the stream of JSON values the node writes back. A node does not
// frame IPC responses with lengths or newlines it promises to keep, so the
// stream is decoded value by value, which is what every client library does.
func (c *ipcConn) read(socket net.Conn, m *mux) {
	dec := json.NewDecoder(socket)
	for {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			if err == io.EOF {
				m.fail(errConnectionClosed)
			} else {
				m.fail(fmt.Errorf("IPC read failed: %w", err))
			}
			c.dropIfCurrent(socket)
			return
		}
		m.dispatch(raw)
	}
}

func (c *ipcConn) dropIfCurrent(socket net.Conn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.socket == socket {
		c.socket = nil
		socket.Close()
	}
}

func (c *ipcConn) do(ctx context.Context, payload []byte) attempt {
	ids := payloadIDs(payload)

	socket, m, setup, err := c.connect(ctx)
	if err != nil {
		return attempt{phases: setup, err: err}
	}

	p, err := m.register(ids)
	if err != nil {
		return attempt{phases: setup, err: err}
	}

	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	sendStart := time.Now()
	c.writeMu.Lock()
	_, writeErr := socket.Write(payload)
	c.writeMu.Unlock()
	sent := time.Now()

	if writeErr != nil {
		m.release(ids)
		c.dropIfCurrent(socket)
		return attempt{
			phases:    Phases{Connecting: setup.Connecting, Sending: sent.Sub(sendStart)},
			sentBytes: len(payload),
			err:       fmt.Errorf("IPC write failed: %w", writeErr),
		}
	}

	res := m.await(ctx, p, sent)
	res.phases.Connecting = setup.Connecting
	res.phases.Sending = sent.Sub(sendStart)
	res.sentBytes = len(payload)
	res.reused = setup.Connecting == 0
	return res
}

func (c *ipcConn) Close() error {
	c.mu.Lock()
	socket := c.socket
	c.socket = nil
	c.closed = true
	c.mu.Unlock()

	if socket == nil {
		return nil
	}
	return socket.Close()
}
