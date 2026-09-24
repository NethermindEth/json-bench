// Package ethrpc is the freshness probe's JSON-RPC client plus the response
// normalisation that probe and review share, so a digest computed on one side
// always matches the other.
package ethrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/jsonrpc-bench/runner/freshness/schema"
)

// Options configures a Client. Headers are sent verbatim and never logged.
type Options struct {
	URL     string
	Headers map[string]string
	Timeout time.Duration
}

// Client sends single JSON-RPC calls with no retries and no rate limiting: a
// failed poll is simply followed by the next scheduled one, and a hidden retry
// would move the send timestamp away from what was actually measured.
type Client struct {
	url     string
	headers http.Header
	http    *http.Client
	clock   *schema.Clock
	nextID  atomic.Uint64
}

func NewClient(opts Options, clock *schema.Clock) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 64
	transport.MaxIdleConnsPerHost = 64
	transport.IdleConnTimeout = 30 * time.Second
	// Compression would make response_bytes and body-complete time depend on
	// the server's gzip choice rather than on the payload.
	transport.DisableCompression = true

	headers := make(http.Header, len(opts.Headers)+1)
	for k, v := range opts.Headers {
		headers.Set(k, v)
	}
	headers.Set("Content-Type", "application/json")

	return &Client{
		url:     opts.URL,
		headers: headers,
		http:    &http.Client{Transport: transport, Timeout: opts.Timeout},
		clock:   clock,
	}
}

// Response is the raw outcome of one call. Received is taken as soon as the
// body has been read, before any parsing.
type Response struct {
	Sent       schema.Stamp
	Received   schema.Stamp
	SentAt     time.Time
	HTTPStatus int
	Body       []byte
	Err        error
	Timeout    bool
}

type request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

func (c *Client) Call(ctx context.Context, method string, params ...any) Response {
	if params == nil {
		params = []any{}
	}
	body, err := json.Marshal(request{JSONRPC: "2.0", ID: c.nextID.Add(1), Method: method, Params: params})
	if err != nil {
		return Response{Err: err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return Response{Err: err}
	}
	req.Header = c.headers.Clone()

	sentAt := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		now := time.Now()
		return Response{Sent: c.clock.At(sentAt), SentAt: sentAt, Received: c.clock.At(now), Err: err, Timeout: isTimeout(err)}
	}
	data, err := io.ReadAll(resp.Body)
	now := time.Now()
	resp.Body.Close()
	out := Response{
		Sent:       c.clock.At(sentAt),
		SentAt:     sentAt,
		Received:   c.clock.At(now),
		HTTPStatus: resp.StatusCode,
		Body:       data,
	}
	if err != nil {
		out.Err = err
		out.Timeout = isTimeout(err)
	}
	return out
}

// CallResult is a convenience for non-measured calls (preflight, header
// fetches): it returns the result or an error describing why there is none.
func (c *Client) CallResult(ctx context.Context, method string, params ...any) (json.RawMessage, error) {
	r := Parse(c.Call(ctx, method, params...))
	switch r.Class {
	case schema.ClassResult, schema.ClassNull:
		return r.Result, nil
	default:
		return nil, r.AsError()
	}
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
