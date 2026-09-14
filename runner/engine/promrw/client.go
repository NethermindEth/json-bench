package promrw

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultPushInterval matches k6's remote-write cadence, so dashboard panels
// resolve over the same window they always did.
const DefaultPushInterval = 5 * time.Second

const (
	defaultTimeout  = 10 * time.Second
	defaultAttempts = 3
	maxRetryDelay   = 5 * time.Second
	// maxErrorBodyBytes caps how much of a rejection body is quoted back, so a
	// verbose gateway error cannot flood the log.
	maxErrorBodyBytes = 512
)

// Config describes the remote-write target.
type Config struct {
	// Endpoint is the full write URL, including the write path.
	Endpoint string

	Username string
	Password string

	// BearerToken authenticates instead of basic auth when set.
	BearerToken string

	// Headers are added to every push, which is how a multi-tenant backend is
	// addressed: Mimir and Cortex route on X-Scope-OrgID.
	Headers map[string]string

	PushInterval time.Duration
	Timeout      time.Duration
	UserAgent    string
	Attempts     int
}

// Client pushes series to a remote-write endpoint.
type Client struct {
	endpoint  string
	headers   http.Header
	http      *http.Client
	attempts  int
	userAgent string
}

func New(cfg Config) (*Client, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("remote-write endpoint is required")
	}
	if cfg.Username != "" && cfg.BearerToken != "" {
		return nil, fmt.Errorf("remote-write accepts basic auth or a bearer token, not both")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	attempts := cfg.Attempts
	if attempts <= 0 {
		attempts = defaultAttempts
	}
	userAgent := cfg.UserAgent
	if userAgent == "" {
		userAgent = "jsonrpc-bench"
	}

	headers := http.Header{}
	for name, value := range cfg.Headers {
		headers.Set(name, value)
	}
	headers.Set("Content-Encoding", "snappy")
	headers.Set("Content-Type", "application/x-protobuf")
	headers.Set("X-Prometheus-Remote-Write-Version", "0.1.0")
	headers.Set("User-Agent", userAgent)
	switch {
	case cfg.BearerToken != "":
		headers.Set("Authorization", "Bearer "+cfg.BearerToken)
	case cfg.Username != "":
		headers.Set("Authorization", "Basic "+basicAuth(cfg.Username, cfg.Password))
	}

	return &Client{
		endpoint:  cfg.Endpoint,
		headers:   headers,
		http:      &http.Client{Timeout: timeout},
		attempts:  attempts,
		userAgent: userAgent,
	}, nil
}

// Push writes one batch. A 4xx other than 429 is permanent and returned
// immediately; 5xx and 429 are retried with backoff.
func (c *Client) Push(ctx context.Context, series []Series) error {
	if len(series) == 0 {
		return nil
	}
	body := Encode(series)

	var lastErr error
	for attempt := 1; attempt <= c.attempts; attempt++ {
		if attempt > 1 {
			delay := backoff(attempt - 1)
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			}
			timer.Stop()
		}

		retryable, err := c.push(ctx, body)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retryable {
			return err
		}
	}
	return fmt.Errorf("remote write failed after %d attempts: %w", c.attempts, lastErr)
}

func (c *Client) push(ctx context.Context, body []byte) (retryable bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("failed to build remote-write request: %w", err)
	}
	req.Header = c.headers.Clone()
	req.ContentLength = int64(len(body))

	resp, err := c.http.Do(req)
	if err != nil {
		return true, fmt.Errorf("remote write request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 == 2 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return false, nil
	}

	detail, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	message := strings.TrimSpace(string(detail))
	retryable = resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests
	return retryable, fmt.Errorf("remote write rejected with status %d: %s", resp.StatusCode, message)
}

func backoff(attempt int) time.Duration {
	delay := 200 * time.Millisecond
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= maxRetryDelay {
			return maxRetryDelay
		}
	}
	return delay
}

func basicAuth(username, password string) string {
	return base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
}
