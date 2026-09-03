package comparator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/time/rate"

	"github.com/jsonrpc-bench/runner/types"
)

// Transport error classes recorded per client when a call never produced a
// response, so a run lost to throttling is distinguishable from a broken node.
const (
	// TransportRateLimited means the endpoint kept answering 429 until the
	// attempt budget ran out.
	TransportRateLimited = "rate_limited"
	// TransportTimeout means the request timed out or was cancelled.
	TransportTimeout = "timeout"
	// TransportTruncated means a 200 response carried a body that is not valid
	// JSON, which in practice means it was cut short in transit.
	TransportTruncated = "truncated_body"
	// TransportOther covers every other transport failure.
	TransportOther = "other"
)

// maxRetryDelay caps both the exponential backoff and a server-supplied
// Retry-After, so a hostile or mistaken header cannot stall a run.
const maxRetryDelay = 30 * time.Second

// transportError carries the class of a terminal transport failure alongside
// the message that gets reported to the operator.
type transportError struct {
	class   string
	message string
}

func (e *transportError) Error() string { return e.message }

// classifyTransportError returns the class recorded for a failed call.
func classifyTransportError(err error) string {
	var te *transportError
	if errors.As(err, &te) {
		return te.class
	}
	return TransportOther
}

// rpcTransport sends JSON-RPC calls to one client. It owns that client's HTTP
// connection pool and its rate limiter, so a slow reference endpoint throttles
// only itself.
type rpcTransport struct {
	name      string
	url       string
	http      *http.Client
	limiter   *rate.Limiter
	attempts  int
	baseDelay time.Duration
	verbose   bool
}

// newRPCTransport builds the transport for one client. The rate limit is taken
// from cfg.RateLimitRPS when set (the --rate-limit flag), else from the
// client's own rate_limit block in clients.yaml; an unset limit means no cap.
func newRPCTransport(cfg *ComparisonConfig, client *types.ClientConfig, attempts int, baseDelay time.Duration) *rpcTransport {
	t := &rpcTransport{
		name: client.Name,
		url:  client.URL,
		http: &http.Client{
			Timeout:   time.Duration(cfg.TimeoutSeconds) * time.Second,
			Transport: freshConnTransport(),
		},
		attempts:  attempts,
		baseDelay: baseDelay,
		verbose:   cfg.Verbose,
	}

	rps := cfg.RateLimitRPS
	burst := 1
	if rps <= 0 && client.RateLimit != nil && client.RateLimit.RequestsPerSecond > 0 {
		rps = float64(client.RateLimit.RequestsPerSecond)
		if client.RateLimit.Burst > 0 {
			burst = client.RateLimit.Burst
		}
	}
	if rps > 0 {
		t.limiter = rate.NewLimiter(rate.Limit(rps), burst)
	}
	return t
}

// freshConnTransport keeps pooled connections short-lived. A node closes idle
// keep-alive connections on its own schedule; the default 90s IdleConnTimeout
// will happily hand a half-closed one to the next request, and Go does not
// silently retry a POST the way it retries idempotent methods. The result is a
// body cut short mid-JSON, which looks like a broken endpoint rather than a
// stale socket. Retiring idle connections quickly removes the class.
func freshConnTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.IdleConnTimeout = 5 * time.Second
	return t
}

// call sends a JSON-RPC request, retrying transport errors, 5xx and 429 with
// exponential backoff up to the attempt budget. A 200 response carrying a
// JSON-RPC error object is returned as a valid response (not retried); any
// other 4xx is a hard failure.
func (t *rpcTransport) call(method string, params []interface{}) (map[string]interface{}, error) {
	requestJSON, err := json.Marshal(JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      1,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	if t.verbose {
		log.Printf("JSON-RPC Request to %s: %s", t.url, formatCurlCommand(t.url, requestJSON))
	}

	attempts := t.attempts
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	delay := time.Duration(0)
	for attempt := 0; attempt < attempts; attempt++ {
		if delay > 0 {
			time.Sleep(delay)
			delay = 0
		}
		if t.limiter != nil {
			if err := t.limiter.Wait(context.Background()); err != nil {
				return nil, &transportError{class: TransportOther, message: fmt.Sprintf("rate limiter wait failed: %v", err)}
			}
		}

		req, err := http.NewRequest("POST", t.url, bytes.NewBuffer(requestJSON))
		if err != nil {
			return nil, fmt.Errorf("failed to create HTTP request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := t.http.Do(req)
		if err != nil {
			lastErr = &transportError{class: transportClassForError(err), message: fmt.Sprintf("HTTP request failed: %v", err)}
			delay = backoffDelay(t.baseDelay, attempt+1)
			continue
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = &transportError{class: transportClassForError(readErr), message: fmt.Sprintf("failed to read response body: %v", readErr)}
			delay = backoffDelay(t.baseDelay, attempt+1)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			class := TransportOther
			if resp.StatusCode == http.StatusTooManyRequests {
				class = TransportRateLimited
			}
			lastErr = &transportError{
				class:   class,
				message: fmt.Sprintf("HTTP request failed with status %d: %s", resp.StatusCode, string(body)),
			}
			if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
				delay = retryDelay(t.baseDelay, attempt+1, resp.Header.Get("Retry-After"))
				continue
			}
			return nil, lastErr
		}

		var rawResponse map[string]interface{}
		if err := json.Unmarshal(body, &rawResponse); err != nil {
			// A 200 whose body will not parse is a truncated read, not a node
			// that disagrees. Retry it like any other transport fault — returning
			// here would spend none of the attempt budget and silently drop the
			// call from the comparison.
			lastErr = &transportError{
				class:   TransportTruncated,
				message: fmt.Sprintf("failed to parse response (%d bytes): %v", len(body), err),
			}
			delay = backoffDelay(t.baseDelay, attempt+1)
			continue
		}
		return rawResponse, nil
	}

	return nil, lastErr
}

// transportClassForError distinguishes a timeout/cancellation from any other
// transport failure.
func transportClassForError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return TransportTimeout
	}
	var netErr interface{ Timeout() bool }
	if errors.As(err, &netErr) && netErr.Timeout() {
		return TransportTimeout
	}
	return TransportOther
}

// retryDelay is the wait before the next attempt: the exponential backoff, or
// the server's Retry-After when that asks for longer.
func retryDelay(base time.Duration, attempt int, retryAfter string) time.Duration {
	delay := backoffDelay(base, attempt)
	if after, ok := parseRetryAfter(retryAfter, time.Now()); ok && after > delay {
		delay = after
	}
	if delay > maxRetryDelay {
		delay = maxRetryDelay
	}
	return delay
}

// parseRetryAfter reads a Retry-After header in either of its forms:
// delta-seconds or an HTTP date.
func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds < 0 {
			return 0, false
		}
		return time.Duration(seconds) * time.Second, true
	}
	if when, err := http.ParseTime(value); err == nil {
		if d := when.Sub(now); d > 0 {
			return d, true
		}
		return 0, true
	}
	return 0, false
}

// backoffDelay returns the wait before a given (1-based) retry attempt: base,
// 2*base, 4*base, ... capped at maxRetryDelay, with up to 25% added jitter so
// concurrent callers do not retry in lockstep.
func backoffDelay(base time.Duration, attempt int) time.Duration {
	if base <= 0 || attempt < 1 {
		return 0
	}
	delay := base
	for i := 1; i < attempt && delay < maxRetryDelay; i++ {
		delay *= 2
	}
	delay += time.Duration(rand.Int63n(int64(delay)/4 + 1))
	if delay > maxRetryDelay {
		delay = maxRetryDelay
	}
	return delay
}
