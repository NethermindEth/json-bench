package engine

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"sync"
	"time"

	"github.com/jsonrpc-bench/runner/types"
)

// idleConnTimeout retires pooled connections quickly. A node closes idle
// keep-alives on its own schedule, and Go will hand a half-closed one to the
// next request without retrying the POST; the result is a body cut short
// mid-JSON that looks like a broken endpoint rather than a stale socket.
const idleConnTimeout = 5 * time.Second

// TransportOptions are the settings that decide whether measurements are
// comparable to a previous run. Each default here is a deliberate choice, not
// Go's, and the effective values are recorded in the run's provenance.
type TransportOptions struct {
	// AcceptCompression asks the node to compress responses. Off by default
	// because k6 sends no Accept-Encoding at all, while Go's transport adds
	// gzip unless told not to — leaving it on would silently change the latency
	// and byte counts of every large response against earlier baselines.
	AcceptCompression bool

	// ReuseConnections keeps the connection pool warm, as k6 does. Turning it
	// off measures cold-connection cost instead.
	ReuseConnections bool

	// HTTP2 permits an h2 upgrade over TLS. Off by default: multiplexing changes
	// the concurrency model, so it must be an explicit choice rather than an
	// accident of the URL scheme.
	HTTP2 bool

	Timeout time.Duration
}

func DefaultTransportOptions() TransportOptions {
	return TransportOptions{ReuseConnections: true, Timeout: 30 * time.Second}
}

// conn carries one request to a node and brings back what it answered. HTTP
// gives a request its own exchange; WebSocket and IPC multiplex many over one
// connection and match responses by JSON-RPC id.
type conn interface {
	do(ctx context.Context, payload []byte) attempt
	Close() error
}

// target sends requests to one client, owning that client's connection.
type target struct {
	name       string
	clientType string
	url        string
	transport  TransportKind
	headers    map[string]string
	conn       conn
}

func (t *target) do(ctx context.Context, payload []byte) attempt {
	return t.conn.do(ctx, payload)
}

// Close releases the target's connection. HTTP keeps a pool that the runtime
// would reclaim anyway; a WebSocket or IPC connection is a live socket the node
// sees, so leaving it open outlasts the run.
func (t *target) Close() error {
	if t.conn == nil {
		return nil
	}
	return t.conn.Close()
}

func newTarget(client *types.ClientConfig, concurrency int, opts TransportOptions) (*target, error) {
	timeout := opts.Timeout
	if client.Timeout != "" {
		parsed, err := time.ParseDuration(client.Timeout)
		if err != nil {
			return nil, fmt.Errorf("client %s has an invalid timeout %q: %w", client.Name, client.Timeout, err)
		}
		timeout = parsed
	}

	if concurrency < 1 {
		concurrency = 1
	}

	kind, err := TransportKindFor(client.URL)
	if err != nil {
		return nil, fmt.Errorf("client %s: %w", client.Name, err)
	}

	headers := map[string]string{"Content-Type": "application/json"}
	for name, value := range client.Headers {
		headers[name] = value
	}
	if err := applyAuth(headers, client); err != nil {
		return nil, err
	}

	tgt := &target{
		name:       client.Name,
		clientType: client.Type,
		url:        client.URL,
		transport:  kind,
		headers:    headers,
	}

	switch kind {
	case TransportWebSocket:
		tgt.conn = newWebSocketConn(client.URL, headers, timeout, opts)
		return tgt, nil
	case TransportIPC:
		tgt.conn = newIPCConn(ipcPath(client.URL), timeout)
		return tgt, nil
	}

	transport := &http.Transport{
		// Go defaults MaxIdleConnsPerHost to 2, which turns a concurrent load
		// test into connection churn: the pool cannot hold the connections in
		// flight, so most requests dial afresh and pay for it in blocked time.
		MaxIdleConnsPerHost:   concurrency,
		MaxConnsPerHost:       concurrency,
		MaxIdleConns:          concurrency,
		IdleConnTimeout:       idleConnTimeout,
		DisableCompression:    !opts.AcceptCompression,
		DisableKeepAlives:     !opts.ReuseConnections,
		ForceAttemptHTTP2:     opts.HTTP2,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}

	tgt.conn = &httpConn{
		url:     client.URL,
		headers: headers,
		client:  &http.Client{Timeout: timeout, Transport: transport},
	}
	return tgt, nil
}

// applyAuth turns the client's auth block into request headers. The registry
// already rejects an auth block missing its credential.
func applyAuth(headers map[string]string, client *types.ClientConfig) error {
	if client.Auth == nil {
		return nil
	}
	switch client.Auth.Type {
	case "basic":
		headers["Authorization"] = "Basic " + basicAuthValue(client.Auth.Username, client.Auth.Password)
	case "bearer":
		headers["Authorization"] = "Bearer " + client.Auth.Token
	case "api_key":
		headers["X-API-Key"] = client.Auth.APIKey
	default:
		return fmt.Errorf("client %s has an unsupported auth type %q", client.Name, client.Auth.Type)
	}
	return nil
}

// attempt is what one request produced, whether or not it reached the node.
type attempt struct {
	status    int
	body      []byte
	phases    Phases
	reused    bool
	sentBytes int
	err       error
}

// httpConn gives every request its own exchange over a pooled connection.
type httpConn struct {
	url     string
	headers map[string]string
	client  *http.Client
}

func (h *httpConn) Close() error {
	h.client.CloseIdleConnections()
	return nil
}

func (h *httpConn) do(ctx context.Context, payload []byte) attempt {
	tr := &tracer{}
	req, err := http.NewRequestWithContext(
		httptrace.WithClientTrace(ctx, tr.clientTrace()),
		http.MethodPost, h.url, bytes.NewReader(payload))
	if err != nil {
		return attempt{err: fmt.Errorf("failed to build request: %w", err)}
	}
	for name, value := range h.headers {
		req.Header.Set(name, value)
	}
	req.ContentLength = int64(len(payload))

	sent := len(payload) + estimateHeaderBytes(req)

	resp, err := h.client.Do(req)
	if err != nil {
		return attempt{phases: tr.phases(time.Time{}), reused: tr.connectionReused(), sentBytes: sent, err: err}
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)
	done := time.Now()
	if readErr != nil {
		return attempt{
			status: resp.StatusCode, phases: tr.phases(done), reused: tr.connectionReused(), sentBytes: sent,
			err: fmt.Errorf("failed to read response body: %w", readErr),
		}
	}

	return attempt{
		status:    resp.StatusCode,
		body:      body,
		phases:    tr.phases(done),
		reused:    tr.connectionReused(),
		sentBytes: sent,
	}
}

// tracer records the transition points httptrace reports for one request.
//
// The callbacks do not all run on the goroutine that called Do, and they do not
// all run before it returns: Go's transport dials on a goroutine of its own and
// writes the request from the connection's write loop, and a request that got a
// pooled connection or failed early returns while those are still going. So the
// fields are guarded — the alternative was a data race on every dial that lost
// the pool race.
type tracer struct {
	mu        sync.Mutex
	getConn   time.Time
	gotConn   time.Time
	dnsStart  time.Time
	dnsDone   time.Time
	connStart time.Time
	connDone  time.Time
	tlsStart  time.Time
	tlsDone   time.Time
	wrote     time.Time
	firstByte time.Time
	reused    bool
}

func (tr *tracer) mark(field *time.Time) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	*field = time.Now()
}

// markFirst keeps the earliest of several reports. A dial walks the addresses
// the name resolved to, so connecting starts once and finishes once per
// attempt, and the span that matters is the whole walk.
func (tr *tracer) markFirst(field *time.Time) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if field.IsZero() {
		*field = time.Now()
	}
}

func (tr *tracer) clientTrace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		GetConn:           func(string) { tr.mark(&tr.getConn) },
		DNSStart:          func(httptrace.DNSStartInfo) { tr.mark(&tr.dnsStart) },
		DNSDone:           func(httptrace.DNSDoneInfo) { tr.mark(&tr.dnsDone) },
		ConnectStart:      func(string, string) { tr.markFirst(&tr.connStart) },
		ConnectDone:       func(string, string, error) { tr.mark(&tr.connDone) },
		TLSHandshakeStart: func() { tr.mark(&tr.tlsStart) },
		TLSHandshakeDone:  func(tls.ConnectionState, error) { tr.mark(&tr.tlsDone) },
		GotConn: func(info httptrace.GotConnInfo) {
			tr.mu.Lock()
			defer tr.mu.Unlock()
			tr.gotConn, tr.reused = time.Now(), info.Reused
		},
		WroteRequest:         func(httptrace.WroteRequestInfo) { tr.mark(&tr.wrote) },
		GotFirstResponseByte: func() { tr.mark(&tr.firstByte) },
	}
}

// phases maps the trace points onto the k6 metric family:
//
//	blocked          GetConn              -> GotConn   (includes dial and TLS)
//	connecting       ConnectStart         -> ConnectDone
//	tls_handshaking  TLSHandshakeStart    -> TLSHandshakeDone
//	sending          GotConn              -> WroteRequest
//	waiting          WroteRequest         -> GotFirstResponseByte
//	receiving        GotFirstResponseByte -> last byte read
func (tr *tracer) phases(done time.Time) Phases {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return Phases{
		Blocked:    span(tr.getConn, tr.gotConn),
		DNS:        span(tr.dnsStart, tr.dnsDone),
		Connecting: span(tr.connStart, tr.connDone),
		TLS:        span(tr.tlsStart, tr.tlsDone),
		Sending:    span(tr.gotConn, tr.wrote),
		Waiting:    span(tr.wrote, tr.firstByte),
		Receiving:  span(tr.firstByte, done),
	}
}

func (tr *tracer) connectionReused() bool {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return tr.reused
}

func basicAuthValue(username, password string) string {
	return base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
}

func span(from, to time.Time) time.Duration {
	if from.IsZero() || to.IsZero() || !to.After(from) {
		return 0
	}
	return to.Sub(from)
}

// estimateHeaderBytes approximates the request line and headers so data_sent
// counts wire bytes rather than body length, as k6 does.
func estimateHeaderBytes(req *http.Request) int {
	n := len(req.Method) + len(req.URL.RequestURI()) + len("  HTTP/1.1\r\n")
	n += len("Host: ") + len(req.URL.Host) + len("\r\n")
	for name, values := range req.Header {
		for _, value := range values {
			n += len(name) + len(": ") + len(value) + len("\r\n")
		}
	}
	return n + len("\r\n")
}
