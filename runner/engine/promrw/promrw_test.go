package promrw

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/internal/promsink"
)

// The encoder is checked against the decoder that read k6's real remote-write
// payloads, so agreement means the wire format is right rather than merely
// self-consistent.
func TestEncodeIsReadableByTheDecoder(t *testing.T) {
	in := []Series{
		{
			Name:        "bench_http_req_duration_p99",
			Labels:      map[string]string{"testid": "t", "scenario": "nethermind", "rpc_method": "eth_call", "status": "200"},
			Value:       0.0429,
			TimestampMS: 1730000000000,
		},
		{
			Name:        "bench_http_reqs_total",
			Labels:      map[string]string{"testid": "t", "scenario": "nethermind", "outcome": "rpc_error"},
			Value:       142,
			TimestampMS: 1730000000000,
		},
	}

	out, err := promsink.Decode(Encode(in))
	require.NoError(t, err)
	require.Len(t, out, 2)

	assert.Equal(t, "bench_http_req_duration_p99", out[0].Name())
	assert.Equal(t, []string{"rpc_method", "scenario", "status", "testid"}, out[0].LabelKeys())
	assert.InDelta(t, 0.0429, out[0].Samples[0].Value, 1e-12)
	assert.EqualValues(t, 1730000000000, out[0].Samples[0].TimestampMS)

	assert.Equal(t, "bench_http_reqs_total", out[1].Name())
	assert.Equal(t, "rpc_error", out[1].Labels["outcome"])
	assert.EqualValues(t, 142, out[1].Samples[0].Value)
}

func TestEncodeCarriesTheMetricNameAsALabel(t *testing.T) {
	out, err := promsink.Decode(Encode([]Series{{Name: "bench_inflight", Value: 3}}))
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "bench_inflight", out[0].Labels["__name__"])
}

func TestEncodeDropsEmptyLabelValues(t *testing.T) {
	out, err := promsink.Decode(Encode([]Series{{
		Name:   "bench_inflight",
		Labels: map[string]string{"scenario": "geth", "client_type": ""},
		Value:  1,
	}}))
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, []string{"scenario"}, out[0].LabelKeys(),
		"an empty label value must be omitted rather than written as an empty string")
}

// A NaN or an infinity is stored by Prometheus as a real sample and then
// poisons every aggregation over the series, so neither reaches the wire.
func TestEncodeSanitizesNonFiniteValues(t *testing.T) {
	out, err := promsink.Decode(Encode([]Series{
		{Name: "a", Value: math.NaN()},
		{Name: "b", Value: math.Inf(1)},
		{Name: "c", Value: math.Inf(-1)},
		{Name: "d", Value: 1.5},
	}))
	require.NoError(t, err)
	require.Len(t, out, 4)

	assert.Zero(t, out[0].Samples[0].Value)
	assert.Zero(t, out[1].Samples[0].Value)
	assert.Zero(t, out[2].Samples[0].Value)
	assert.InDelta(t, 1.5, out[3].Samples[0].Value, 1e-12)
}

func recordingEndpoint(t *testing.T) (string, func() []*http.Request) {
	t.Helper()
	var (
		mu   sync.Mutex
		seen []*http.Request
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Clone(context.Background()))
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, func() []*http.Request {
		mu.Lock()
		defer mu.Unlock()
		return seen
	}
}

func TestPushSendsTheContractHeaders(t *testing.T) {
	endpoint, requests := recordingEndpoint(t)

	client, err := New(Config{Endpoint: endpoint, Username: "u", Password: "p", UserAgent: "jsonrpc-bench/1"})
	require.NoError(t, err)
	require.NoError(t, client.Push(context.Background(), []Series{{Name: "bench_inflight", Value: 1}}))

	require.Len(t, requests(), 1)
	header := requests()[0].Header

	assert.Equal(t, "snappy", header.Get("Content-Encoding"))
	assert.Equal(t, "application/x-protobuf", header.Get("Content-Type"))
	assert.Equal(t, "0.1.0", header.Get("X-Prometheus-Remote-Write-Version"))
	assert.Equal(t, "jsonrpc-bench/1", header.Get("User-Agent"))

	username, password, ok := requests()[0].BasicAuth()
	require.True(t, ok)
	assert.Equal(t, "u", username)
	assert.Equal(t, "p", password)
}

func TestPushSupportsBearerAndCustomHeaders(t *testing.T) {
	endpoint, requests := recordingEndpoint(t)

	client, err := New(Config{
		Endpoint:    endpoint,
		BearerToken: "tok",
		Headers:     map[string]string{"X-Scope-OrgID": "team"},
	})
	require.NoError(t, err)
	require.NoError(t, client.Push(context.Background(), []Series{{Name: "bench_inflight", Value: 1}}))

	header := requests()[0].Header
	assert.Equal(t, "Bearer tok", header.Get("Authorization"))
	assert.Equal(t, "team", header.Get("X-Scope-OrgID"), "multi-tenant backends route on this header")
}

func TestNewRejectsConflictingAuth(t *testing.T) {
	_, err := New(Config{Endpoint: "http://x", Username: "u", BearerToken: "t"})
	assert.Error(t, err)
}

func TestNewRequiresAnEndpoint(t *testing.T) {
	_, err := New(Config{})
	assert.Error(t, err)
}

func TestPushRetriesTransientRejections(t *testing.T) {
	var attempts int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		n := attempts
		mu.Unlock()
		if n < 3 {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client, err := New(Config{Endpoint: srv.URL, Attempts: 3})
	require.NoError(t, err)
	require.NoError(t, client.Push(context.Background(), []Series{{Name: "bench_inflight", Value: 1}}))

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 3, attempts)
}

// A rejected payload is the client's fault and will be rejected again, so it is
// not retried: doing so would spend the attempt budget and delay the run.
func TestPushDoesNotRetryPermanentRejections(t *testing.T) {
	var attempts int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		mu.Unlock()
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()

	client, err := New(Config{Endpoint: srv.URL, Attempts: 3})
	require.NoError(t, err)

	err = client.Push(context.Background(), []Series{{Name: "bench_inflight", Value: 1}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, attempts)
}

func TestPushHonoursCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client, err := New(Config{Endpoint: srv.URL, Attempts: 10})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = client.Push(ctx, []Series{{Name: "bench_inflight", Value: 1}})
	require.Error(t, err)
	assert.Less(t, time.Since(start), 3*time.Second)
}

func TestPushOfNothingIsANoOp(t *testing.T) {
	endpoint, requests := recordingEndpoint(t)

	client, err := New(Config{Endpoint: endpoint})
	require.NoError(t, err)
	require.NoError(t, client.Push(context.Background(), nil))

	assert.Empty(t, requests())
}
