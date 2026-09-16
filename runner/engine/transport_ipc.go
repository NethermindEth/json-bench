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
	dial    *ipcDial
	writeMu sync.Mutex
	closed  bool
}

// ipcDial is one connection attempt several requests are waiting on. See
// wsDial: the fields are written before done is closed and read only after.
type ipcDial struct {
	done    chan struct{}
	socket  net.Conn
	elapsed time.Duration
	err     error
}

func newIPCConn(path string, timeout time.Duration) *ipcConn {
	return &ipcConn{path: path, timeout: timeout}
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

// connect returns the live socket and the mux belonging to it. The mux is per
// socket: a reader blocked on a socket that has since been replaced must not be
// able to fail the requests running over its replacement.
func (c *ipcConn) connect(ctx context.Context) (net.Conn, *mux, Phases, error) {
	for {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return nil, nil, Phases{}, errConnectionClosed
		}
		if c.socket != nil {
			socket, m := c.socket, c.mux
			c.mu.Unlock()
			return socket, m, Phases{}, nil
		}
		if inProgress := c.dial; inProgress != nil {
			// Waiting on the attempt rather than on c.mu, which a context
			// cannot interrupt.
			c.mu.Unlock()
			select {
			case <-inProgress.done:
				if inProgress.err != nil {
					return nil, nil, Phases{}, inProgress.err
				}
				continue
			case <-ctx.Done():
				return nil, nil, Phases{}, ctx.Err()
			}
		}

		if err := checkIPCPath(c.path); err != nil {
			c.mu.Unlock()
			return nil, nil, Phases{}, err
		}

		current := &ipcDial{done: make(chan struct{})}
		c.dial = current
		c.mu.Unlock()

		dialer := &net.Dialer{Timeout: c.timeout}
		start := time.Now()
		current.socket, current.err = dialer.DialContext(ctx, "unix", c.path)
		current.elapsed = time.Since(start)
		if current.err != nil {
			current.err = fmt.Errorf("failed to connect to the IPC socket %s: %w", c.path, current.err)
		}

		var m *mux
		c.mu.Lock()
		c.dial = nil
		if current.err == nil {
			if c.closed {
				current.err = errConnectionClosed
			} else {
				m = newMux()
				c.socket = current.socket
				c.mux = m
				go c.read(current.socket, m)
			}
		}
		c.mu.Unlock()
		close(current.done)

		if current.err != nil {
			if current.socket != nil {
				current.socket.Close()
			}
			return nil, nil, Phases{}, current.err
		}

		return current.socket, m, Phases{Connecting: current.elapsed}, nil
	}
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
		c.mux = nil
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
	// See the WebSocket transport: an unbounded write holds the mutex and
	// stalls every other request on the connection.
	writeErr := socket.SetWriteDeadline(writeDeadline(ctx, c.timeout))
	if writeErr == nil {
		_, writeErr = socket.Write(payload)
	}
	c.writeMu.Unlock()
	sent := time.Now()

	if writeErr != nil {
		m.release(ids)
		// Closing the socket wakes its reader, which is what fails the requests
		// still waiting on this connection instead of leaving them to time out.
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
	c.mux = nil
	c.closed = true
	c.mu.Unlock()

	if socket == nil {
		return nil
	}
	return socket.Close()
}
