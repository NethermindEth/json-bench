package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/types"
)

// recordHeaders serves a valid JSON-RPC response and keeps the request headers.
func recordHeaders(t *testing.T) (*httptest.Server, func() http.Header) {
	t.Helper()
	var (
		mu   sync.Mutex
		seen http.Header
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = r.Header.Clone()
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))
	}))
	t.Cleanup(srv.Close)
	return srv, func() http.Header {
		mu.Lock()
		defer mu.Unlock()
		return seen
	}
}

// k6 sends no Accept-Encoding, while Go's transport adds gzip unless told not
// to. Left at Go's default the node would compress large responses, changing
// both latency and byte counts against any earlier measurement — a divergence
// with no error to reveal it.
func TestTransportSendsNoAcceptEncodingByDefault(t *testing.T) {
	srv, headers := recordHeaders(t)

	tgt, err := newTarget(&types.ClientConfig{Name: "c", URL: srv.URL}, 4, DefaultTransportOptions())
	require.NoError(t, err)

	res := tgt.do(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}`))
	require.NoError(t, res.err)
	require.Equal(t, http.StatusOK, res.status)

	assert.Empty(t, headers().Get("Accept-Encoding"),
		"Go adds gzip unless DisableCompression is set; k6 sends none")
	assert.Equal(t, "application/json", headers().Get("Content-Type"))
}

func TestTransportCanOptIntoCompression(t *testing.T) {
	srv, headers := recordHeaders(t)

	opts := DefaultTransportOptions()
	opts.AcceptCompression = true
	tgt, err := newTarget(&types.ClientConfig{Name: "c", URL: srv.URL}, 4, opts)
	require.NoError(t, err)

	res := tgt.do(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}`))
	require.NoError(t, res.err)
	assert.Contains(t, headers().Get("Accept-Encoding"), "gzip")
}

// The registry has always accepted headers and auth for a client; the k6 script
// ignored both, so they were configuration that did nothing.
func TestTransportAppliesHeadersAndAuth(t *testing.T) {
	cases := map[string]struct {
		client *types.ClientConfig
		expect map[string]string
	}{
		"custom headers": {
			client: &types.ClientConfig{Name: "c", Headers: map[string]string{"X-Tenant": "nethermind"}},
			expect: map[string]string{"X-Tenant": "nethermind"},
		},
		"basic auth": {
			client: &types.ClientConfig{Name: "c", Auth: &types.AuthConfig{Type: "basic", Username: "u", Password: "p"}},
			expect: map[string]string{"Authorization": "Basic dTpw"},
		},
		"bearer auth": {
			client: &types.ClientConfig{Name: "c", Auth: &types.AuthConfig{Type: "bearer", Token: "tok"}},
			expect: map[string]string{"Authorization": "Bearer tok"},
		},
		"api key": {
			client: &types.ClientConfig{Name: "c", Auth: &types.AuthConfig{Type: "api_key", APIKey: "k"}},
			expect: map[string]string{"X-API-Key": "k"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv, headers := recordHeaders(t)
			tc.client.URL = srv.URL

			tgt, err := newTarget(tc.client, 4, DefaultTransportOptions())
			require.NoError(t, err)

			res := tgt.do(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}`))
			require.NoError(t, res.err)

			for header, want := range tc.expect {
				assert.Equal(t, want, headers().Get(header))
			}
		})
	}
}

func TestTransportRejectsUnusableClientConfig(t *testing.T) {
	t.Run("bad timeout", func(t *testing.T) {
		_, err := newTarget(&types.ClientConfig{Name: "c", URL: "http://x", Timeout: "soon"}, 1, DefaultTransportOptions())
		assert.Error(t, err)
	})

	t.Run("unsupported auth", func(t *testing.T) {
		_, err := newTarget(&types.ClientConfig{
			Name: "c", URL: "http://x",
			Auth: &types.AuthConfig{Type: "kerberos"},
		}, 1, DefaultTransportOptions())
		assert.Error(t, err)
	})
}

func TestTransportRecordsPhases(t *testing.T) {
	srv, _ := recordHeaders(t)

	tgt, err := newTarget(&types.ClientConfig{Name: "c", URL: srv.URL}, 4, DefaultTransportOptions())
	require.NoError(t, err)

	payload := []byte(`{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}`)

	first := tgt.do(context.Background(), payload)
	require.NoError(t, first.err)
	assert.False(t, first.reused, "the first request has no pooled connection to reuse")
	assert.Positive(t, first.phases.Connecting, "a fresh connection must record its dial")

	second := tgt.do(context.Background(), payload)
	require.NoError(t, second.err)
	assert.True(t, second.reused, "keep-alive is on by default, as in k6")
	assert.Zero(t, second.phases.Connecting, "a reused connection does not dial")

	assert.Positive(t, second.phases.Waiting, "time to first byte must be recorded")
	assert.Equal(t,
		second.phases.Sending+second.phases.Waiting+second.phases.Receiving,
		second.phases.Duration(),
		"duration excludes connection setup, matching k6's http_req_duration")
	assert.Greater(t, second.sentBytes, len(payload), "data_sent counts headers, not just the body")
}

func TestTransportCanDisableConnectionReuse(t *testing.T) {
	srv, _ := recordHeaders(t)

	opts := DefaultTransportOptions()
	opts.ReuseConnections = false
	tgt, err := newTarget(&types.ClientConfig{Name: "c", URL: srv.URL}, 4, opts)
	require.NoError(t, err)

	payload := []byte(`{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}`)
	require.NoError(t, tgt.do(context.Background(), payload).err)

	second := tgt.do(context.Background(), payload)
	require.NoError(t, second.err)
	assert.False(t, second.reused)
}
