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

// target sends requests to one client, owning that client's connection pool.
type target struct {
	name       string
	clientType string
	url        string
	headers    map[string]string
	http       *http.Client
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

	headers := map[string]string{"Content-Type": "application/json"}
	for name, value := range client.Headers {
		headers[name] = value
	}
	if err := applyAuth(headers, client); err != nil {
		return nil, err
	}

	return &target{
		name:       client.Name,
		clientType: client.Type,
		url:        client.URL,
		headers:    headers,
		http:       &http.Client{Timeout: timeout, Transport: transport},
	}, nil
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

func (t *target) do(ctx context.Context, payload []byte) attempt {
	tr := &tracer{}
	req, err := http.NewRequestWithContext(
		httptrace.WithClientTrace(ctx, tr.clientTrace()),
		http.MethodPost, t.url, bytes.NewReader(payload))
	if err != nil {
		return attempt{err: fmt.Errorf("failed to build request: %w", err)}
	}
	for name, value := range t.headers {
		req.Header.Set(name, value)
	}
	req.ContentLength = int64(len(payload))

	sent := len(payload) + estimateHeaderBytes(req)

	resp, err := t.http.Do(req)
	if err != nil {
		return attempt{phases: tr.phases(time.Time{}), reused: tr.reused, sentBytes: sent, err: err}
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)
	done := time.Now()
	if readErr != nil {
		return attempt{
			status: resp.StatusCode, phases: tr.phases(done), reused: tr.reused, sentBytes: sent,
			err: fmt.Errorf("failed to read response body: %w", readErr),
		}
	}

	return attempt{
		status:    resp.StatusCode,
		body:      body,
		phases:    tr.phases(done),
		reused:    tr.reused,
		sentBytes: sent,
	}
}

// tracer records the transition points httptrace reports for one request. The
// callbacks all fire before Do returns, on the calling goroutine's request, so
// plain fields are sufficient.
type tracer struct {
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

func (tr *tracer) clientTrace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		GetConn:  func(string) { tr.getConn = time.Now() },
		DNSStart: func(httptrace.DNSStartInfo) { tr.dnsStart = time.Now() },
		DNSDone:  func(httptrace.DNSDoneInfo) { tr.dnsDone = time.Now() },
		ConnectStart: func(string, string) {
			if tr.connStart.IsZero() {
				tr.connStart = time.Now()
			}
		},
		ConnectDone:          func(string, string, error) { tr.connDone = time.Now() },
		TLSHandshakeStart:    func() { tr.tlsStart = time.Now() },
		TLSHandshakeDone:     func(tls.ConnectionState, error) { tr.tlsDone = time.Now() },
		GotConn:              func(info httptrace.GotConnInfo) { tr.gotConn, tr.reused = time.Now(), info.Reused },
		WroteRequest:         func(httptrace.WroteRequestInfo) { tr.wrote = time.Now() },
		GotFirstResponseByte: func() { tr.firstByte = time.Now() },
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
