package stubnode

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func serve(t *testing.T, cfg Config) (*httptest.Server, *Stub) {
	t.Helper()
	stub, err := New(cfg)
	require.NoError(t, err)
	srv := httptest.NewServer(stub.Handler())
	t.Cleanup(srv.Close)
	return srv, stub
}

type reply struct {
	status int
	body   string
	err    error
}

func call(t *testing.T, url, method string, id int) reply {
	t.Helper()
	payload := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":%q,"params":[]}`, id, method)
	resp, err := http.Post(url, "application/json", strings.NewReader(payload))
	if err != nil {
		return reply{err: err}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return reply{status: resp.StatusCode, body: string(body), err: err}
}

// The fixture's whole purpose is that an engine A/B is reproducible: identical
// ids must produce identical responses, and the arrival order must not matter.
func TestResponsesAreFixedByRequestID(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Methods = map[string]Method{
		"eth_call": {RPCErrorRate: 0.25, RPCErrorCode: -32000},
	}

	first := make(map[int]string)
	srvA, _ := serve(t, cfg)
	for id := 1; id <= 40; id++ {
		r := call(t, srvA.URL, "eth_call", id)
		require.NoError(t, r.err)
		first[id] = r.body
	}

	srvB, _ := serve(t, cfg)
	var wg sync.WaitGroup
	second := make(map[int]string)
	var mu sync.Mutex
	for id := 40; id >= 1; id-- {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			r := call(t, srvB.URL, "eth_call", id)
			mu.Lock()
			defer mu.Unlock()
			if r.err != nil {
				t.Errorf("id %d: %v", id, r.err)
				return
			}
			second[id] = r.body
		}(id)
	}
	wg.Wait()

	assert.Equal(t, first, second, "same ids concurrently and in reverse order must replay identically")

	errors := 0
	for _, body := range first {
		if strings.Contains(body, `"error"`) {
			errors++
		}
	}
	assert.NotZero(t, errors, "a 25%% error rate should have produced some JSON-RPC errors")
	assert.NotEqual(t, 40, errors, "a 25%% error rate should not have failed every request")
}

func TestSeedChangesTheSequence(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Methods = map[string]Method{"eth_call": {RPCErrorRate: 0.5}}

	bodies := func(seed int64) []string {
		cfg.Seed = seed
		srv, _ := serve(t, cfg)
		out := make([]string, 0, 30)
		for id := 1; id <= 30; id++ {
			r := call(t, srv.URL, "eth_call", id)
			require.NoError(t, r.err)
			out = append(out, r.body)
		}
		return out
	}

	assert.NotEqual(t, bodies(1), bodies(2))
}

func TestOutcomeClasses(t *testing.T) {
	t.Run("rpc error carries the configured code", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Methods = map[string]Method{"eth_call": {RPCErrorRate: 1, RPCErrorCode: -32015}}
		srv, stub := serve(t, cfg)

		r := call(t, srv.URL, "eth_call", 7)
		require.NoError(t, r.err)
		assert.Equal(t, http.StatusOK, r.status, "an RPC error is still HTTP 200 — the reason HTTP-only accounting misses it")
		assert.Contains(t, r.body, `"code":-32015`)
		assert.EqualValues(t, 1, stub.Stats().ByMethod["eth_call"][OutcomeRPCError])
	})

	t.Run("null result is distinguishable from a value", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Methods = map[string]Method{"eth_getLogs": {NullResultRate: 1}}
		srv, stub := serve(t, cfg)

		r := call(t, srv.URL, "eth_getLogs", 1)
		require.NoError(t, r.err)
		assert.Contains(t, r.body, `"result":null`)
		assert.EqualValues(t, 1, stub.Stats().ByMethod["eth_getLogs"][OutcomeRPCNull])
	})

	t.Run("http fault", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Faults = Faults{HTTP500Rate: 1}
		srv, stub := serve(t, cfg)

		r := call(t, srv.URL, "eth_blockNumber", 1)
		require.NoError(t, r.err)
		assert.Equal(t, http.StatusInternalServerError, r.status)
		assert.EqualValues(t, 1, stub.Stats().ByMethod["eth_blockNumber"][OutcomeHTTPError])
	})

	t.Run("rate limited fault sets Retry-After", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Faults = Faults{HTTP429Rate: 1}
		srv, _ := serve(t, cfg)

		resp, err := http.Post(srv.URL, "application/json",
			strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"eth_call"}`))
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
		assert.Equal(t, "1", resp.Header.Get("Retry-After"))
	})

	t.Run("truncated body is a 200 that will not parse", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Faults = Faults{TruncateRate: 1}
		srv, stub := serve(t, cfg)

		r := call(t, srv.URL, "eth_call", 1)
		if r.err == nil {
			assert.Equal(t, http.StatusOK, r.status)
			var parsed map[string]any
			assert.Error(t, json.Unmarshal([]byte(r.body), &parsed))
		}
		assert.EqualValues(t, 1, stub.Stats().ByMethod["eth_call"][OutcomeTruncated])
	})

	t.Run("batches are refused explicitly", func(t *testing.T) {
		srv, _ := serve(t, DefaultConfig())
		resp, err := http.Post(srv.URL, "application/json",
			strings.NewReader(`[{"jsonrpc":"2.0","id":1,"method":"eth_call"}]`))
		require.NoError(t, err)
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		assert.Contains(t, string(body), "batch requests are not supported")
	})
}

func TestResultBytesPadsTheResponse(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Methods = map[string]Method{"eth_getLogs": {ResultBytes: 2048}}
	srv, _ := serve(t, cfg)

	r := call(t, srv.URL, "eth_getLogs", 1)
	require.NoError(t, r.err)
	assert.Greater(t, len(r.body), 2048)
}

func TestLatencyDistributionIsHonoured(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Default.Latency = &Latency{Kind: LatencyUniform, MinMS: 10, MaxMS: 30}
	stub, err := New(cfg)
	require.NoError(t, err)

	var lo, hi float64 = 1e9, 0
	for id := 1; id <= 500; id++ {
		ms := stub.latency(cfg.Default.Latency, "eth_call", uint64(id)).Seconds() * 1000
		lo, hi = min(lo, ms), max(hi, ms)
	}
	assert.GreaterOrEqual(t, lo, 10.0)
	assert.LessOrEqual(t, hi, 30.0)
	assert.Less(t, lo, 13.0, "500 draws should reach near the lower bound")
	assert.Greater(t, hi, 27.0, "500 draws should reach near the upper bound")
}

func TestInvalidConfigIsRejected(t *testing.T) {
	for name, cfg := range map[string]Config{
		"unknown latency kind": {Default: Method{Latency: &Latency{Kind: "poisson"}}},
		"inverted bounds":      {Default: Method{Latency: &Latency{Kind: LatencyUniform, MinMS: 30, MaxMS: 10}}},
		"rate out of range":    {Default: Method{RPCErrorRate: 1.5}},
		"faults exceed one":    {Faults: Faults{HTTP500Rate: 0.7, TimeoutRate: 0.7}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := New(cfg)
			assert.Error(t, err)
		})
	}
}
