package comparator

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jsonrpc-bench/runner/types"
)

// TestCompareIntegration_RetryAfter429 covers the rate-limited reference case:
// a sustained 429 used to be a hard transport failure, losing the call.
func TestCompareIntegration_RetryAfter429(t *testing.T) {
	for _, tc := range []struct {
		name       string
		retryAfter string
	}{
		{"no header", ""},
		{"delta seconds", "0"},
		{"http date", time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			attempts := 0
			throttled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				req := decodeRPC(t, r)
				if req.Method == "eth_chainId" {
					writeRPCResult(w, req.ID, "0x1")
					return
				}
				mu.Lock()
				attempts++
				n := attempts
				mu.Unlock()
				if n < 3 {
					if tc.retryAfter != "" {
						w.Header().Set("Retry-After", tc.retryAfter)
					}
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = w.Write([]byte(`{"code":-32005,"message":"Too Many Requests"}`))
					return
				}
				writeRPCResult(w, req.ID, "0x123")
			}))
			t.Cleanup(throttled.Close)

			stable := newRPCFake(t, "0x1", func(req rpcRequest) interface{} { return "0x123" })

			comp := newTestComparator(t, []*types.ClientConfig{
				{Name: "throttled", URL: throttled.URL},
				{Name: "stable", URL: stable.URL},
			})
			if _, err := comp.Run(); err != nil {
				t.Fatalf("Run should recover after 429 retries, got: %v", err)
			}
			res := comp.GetResults()[0]
			if len(res.TransportErrors) != 0 {
				t.Errorf("429 should be retried, got transport errors %v", res.TransportErrors)
			}
			s := comp.Summarize()
			if s.Identical != 1 || s.TransportError != 0 || s.RateLimited != 0 {
				t.Errorf("summary = %+v, want 1 identical and no transport errors", s)
			}
		})
	}
}

// TestCompareIntegration_429ExhaustedIsClassified pins the reporting side: a
// 429 that outlives the retry budget is recorded as rate-limited, not as an
// anonymous failure, and the call is never compared.
func TestCompareIntegration_429ExhaustedIsClassified(t *testing.T) {
	throttled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPC(t, r)
		if req.Method == "eth_chainId" {
			writeRPCResult(w, req.ID, "0x1")
			return
		}
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":-32005,"message":"Too Many Requests"}`))
	}))
	t.Cleanup(throttled.Close)

	stable := newRPCFake(t, "0x1", func(req rpcRequest) interface{} { return "0x123" })

	comp := newTestComparator(t, []*types.ClientConfig{
		{Name: "throttled", URL: throttled.URL},
		{Name: "stable", URL: stable.URL},
	})
	if _, err := comp.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	res := comp.GetResults()[0]
	if res.TransportErrorClass["throttled"] != TransportRateLimited {
		t.Errorf("transport error class = %v, want %s", res.TransportErrorClass, TransportRateLimited)
	}
	if len(res.Differences) != 0 {
		t.Errorf("a call that lost a client must not be compared, got %v", res.Differences)
	}
	s := comp.Summarize()
	if s.TransportError != 1 || s.RateLimited != 1 || s.Differ != 0 || s.Identical != 0 {
		t.Errorf("summary = %+v, want 1 transport-error / 1 rate-limited", s)
	}
	if !comp.HasTransportErrors() {
		t.Error("HasTransportErrors should report the lost call (--fail-on-transport-error)")
	}
}

// TestRateLimitThrottlesPerClient checks the limiter actually paces requests.
func TestRateLimitThrottlesPerClient(t *testing.T) {
	var mu sync.Mutex
	var arrivals []time.Time
	srv := newRPCFake(t, "0x1", func(req rpcRequest) interface{} {
		mu.Lock()
		arrivals = append(arrivals, time.Now())
		mu.Unlock()
		return "0x1"
	})

	const calls = 5
	const rps = 50.0
	cfg := &ComparisonConfig{
		Name:             "rate-limit",
		Methods:          make([]string, 0, calls),
		MethodRPCNames:   map[string]string{},
		CustomParameters: map[string][]interface{}{},
		Clients:          []*types.ClientConfig{{Name: "a", URL: srv.URL}, {Name: "b", URL: srv.URL}},
		TimeoutSeconds:   5,
		Concurrency:      calls,
		MaxRetries:       1,
		RetryBaseDelayMs: 1,
		RateLimitRPS:     rps,
		OutputDir:        t.TempDir(),
	}
	for i := 0; i < calls; i++ {
		id := "eth_blockNumber_variant" + string(rune('1'+i))
		cfg.Methods = append(cfg.Methods, id)
		cfg.MethodRPCNames[id] = "eth_blockNumber"
		cfg.CustomParameters[id] = []interface{}{}
	}

	comp, err := NewComparator(cfg)
	if err != nil {
		t.Fatalf("NewComparator: %v", err)
	}
	start := time.Now()
	if _, err := comp.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	elapsed := time.Since(start)

	// Each client has its own bucket of burst 1, so the 5 calls on one client
	// cost at least 4 intervals; the preflight eth_chainId consumes one more.
	minWait := time.Duration(float64(calls) / rps * float64(time.Second))
	if elapsed < minWait {
		t.Errorf("run took %v, want at least %v under a %.0f rps cap", elapsed, minWait, rps)
	}
	mu.Lock()
	got := len(arrivals)
	mu.Unlock()
	if got < calls {
		t.Errorf("server saw %d requests, want at least %d", got, calls)
	}
}

func TestRateLimitFromClientConfig(t *testing.T) {
	client := &types.ClientConfig{
		Name:      "slow",
		URL:       "http://localhost:1",
		RateLimit: &types.RateLimitConfig{RequestsPerSecond: 3, Burst: 2},
	}
	tr := newRPCTransport(&ComparisonConfig{TimeoutSeconds: 1}, client, 1, time.Millisecond)
	if tr.limiter == nil {
		t.Fatal("expected the clients.yaml rate_limit block to be enforced")
	}
	if got := float64(tr.limiter.Limit()); got != 3 {
		t.Errorf("limit = %v, want 3", got)
	}
	if got := tr.limiter.Burst(); got != 2 {
		t.Errorf("burst = %d, want 2", got)
	}

	// The flag wins over the config, and expresses fractional rates the int
	// config field cannot.
	tr = newRPCTransport(&ComparisonConfig{TimeoutSeconds: 1, RateLimitRPS: 2.4}, client, 1, time.Millisecond)
	if got := float64(tr.limiter.Limit()); got != 2.4 {
		t.Errorf("limit = %v, want 2.4 from --rate-limit", got)
	}

	// No limit configured anywhere means no cap.
	tr = newRPCTransport(&ComparisonConfig{TimeoutSeconds: 1}, &types.ClientConfig{Name: "fast", URL: "http://localhost:1"}, 1, time.Millisecond)
	if tr.limiter != nil {
		t.Error("expected no limiter when neither the flag nor the config sets one")
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		value string
		want  time.Duration
		ok    bool
	}{
		{"empty", "", 0, false},
		{"seconds", "5", 5 * time.Second, true},
		{"zero", "0", 0, true},
		{"negative", "-1", 0, false},
		{"garbage", "soon", 0, false},
		{"future date", now.Add(90 * time.Second).Format(http.TimeFormat), 90 * time.Second, true},
		{"past date", now.Add(-time.Minute).Format(http.TimeFormat), 0, true},
	}
	for _, tc := range tests {
		got, ok := parseRetryAfter(tc.value, now)
		if ok != tc.ok || got != tc.want {
			t.Errorf("%s: parseRetryAfter(%q) = (%v, %v), want (%v, %v)", tc.name, tc.value, got, ok, tc.want, tc.ok)
		}
	}
}

func TestRetryDelayHonorsRetryAfterAndCap(t *testing.T) {
	if got := retryDelay(time.Millisecond, 1, "2"); got != 2*time.Second {
		t.Errorf("Retry-After should win over a shorter backoff, got %v", got)
	}
	if got := retryDelay(time.Millisecond, 1, ""); got > 10*time.Millisecond {
		t.Errorf("backoff for the first retry should stay near the base delay, got %v", got)
	}
	if got := retryDelay(time.Second, 1, "600"); got != maxRetryDelay {
		t.Errorf("a huge Retry-After should be capped at %v, got %v", maxRetryDelay, got)
	}
	if got := backoffDelay(time.Hour, 4); got != maxRetryDelay {
		t.Errorf("backoff should cap at %v, got %v", maxRetryDelay, got)
	}
	if got := backoffDelay(0, 3); got != 0 {
		t.Errorf("a zero base delay should not wait, got %v", got)
	}
}

// newTestComparator builds a single-call comparator over the given clients.
func newTestComparator(t *testing.T, clients []*types.ClientConfig) *Comparator {
	t.Helper()
	cfg := &ComparisonConfig{
		Name:             "transport",
		Methods:          []string{"eth_blockNumber_variant1"},
		MethodRPCNames:   map[string]string{"eth_blockNumber_variant1": "eth_blockNumber"},
		CustomParameters: map[string][]interface{}{"eth_blockNumber_variant1": {}},
		Clients:          clients,
		TimeoutSeconds:   5,
		Concurrency:      1,
		MaxRetries:       3,
		RetryBaseDelayMs: 1,
		OutputDir:        t.TempDir(),
	}
	comp, err := NewComparator(cfg)
	if err != nil {
		t.Fatalf("NewComparator: %v", err)
	}
	return comp
}
