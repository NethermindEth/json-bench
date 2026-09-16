package stubnode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

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

		r := call(t, srv.URL, "eth_call", 1)
		require.NoError(t, r.err)
		assert.Equal(t, http.StatusInternalServerError, r.status)
		assert.EqualValues(t, 1, stub.Stats().ByMethod["eth_call"][OutcomeHTTPError])
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

	t.Run("a batch is answered as an array", func(t *testing.T) {
		srv, _ := serve(t, DefaultConfig())
		resp, err := http.Post(srv.URL, "application/json",
			strings.NewReader(`[{"jsonrpc":"2.0","id":1,"method":"eth_call"},{"jsonrpc":"2.0","id":2,"method":"eth_call"}]`))
		require.NoError(t, err)
		defer resp.Body.Close()

		var responses []map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&responses))
		assert.Len(t, responses, 2)
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

// A pre-flight check reads these to establish what a node is, so they answer
// immediately and are not subject to the configured latency or faults: an
// unrelated fault rate must not make a pre-flight check flaky.
func TestIdentityProbesAreExemptFromLatencyAndFaults(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Node = Node{ClientVersion: "Nethermind/v1.31.0", ChainID: 100, HeadBlock: 0x2000}
	cfg.Default.Latency = &Latency{Kind: LatencyFixed, MS: 500}
	cfg.Faults = Faults{HTTP500Rate: 1}
	srv, _ := serve(t, cfg)

	start := time.Now()
	for method, want := range map[string]string{
		"web3_clientVersion": `"Nethermind/v1.31.0"`,
		"eth_chainId":        `"0x64"`,
		"eth_blockNumber":    `"0x2000"`,
		"eth_syncing":        `false`,
	} {
		r := call(t, srv.URL, method, 1)
		require.NoError(t, r.err, method)
		assert.Equal(t, http.StatusOK, r.status, method)
		assert.Contains(t, r.body, want, method)
	}
	assert.Less(t, time.Since(start), 400*time.Millisecond, "probes must not pay the configured latency")
}

func TestSyncingNodeReportsProgress(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Node.Syncing = true
	srv, _ := serve(t, cfg)

	r := call(t, srv.URL, "eth_syncing", 1)
	require.NoError(t, r.err)
	assert.Contains(t, r.body, "highestBlock")
}

func TestProbeErrorModelsAnUnanswerableNode(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Node.ProbeError = true
	srv, _ := serve(t, cfg)

	r := call(t, srv.URL, "eth_chainId", 1)
	require.NoError(t, r.err)
	assert.Equal(t, http.StatusInternalServerError, r.status)
}

func TestBatchResponses(t *testing.T) {
	postBatch := func(t *testing.T, url, body string) (int, []byte) {
		t.Helper()
		resp, err := http.Post(url, "application/json", strings.NewReader(body))
		require.NoError(t, err)
		defer resp.Body.Close()
		out, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp.StatusCode, out
	}

	// The spec does not promise an order, so the stub returns them reversed: a
	// client matching by position rather than by id fails here rather than in
	// production.
	t.Run("responses are not in request order", func(t *testing.T) {
		srv, _ := serve(t, DefaultConfig())
		_, body := postBatch(t, srv.URL,
			`[{"jsonrpc":"2.0","id":1,"method":"eth_call"},{"jsonrpc":"2.0","id":2,"method":"eth_call"},{"jsonrpc":"2.0","id":3,"method":"eth_call"}]`)

		var responses []struct {
			ID int `json:"id"`
		}
		require.NoError(t, json.Unmarshal(body, &responses))
		require.Len(t, responses, 3)
		assert.Equal(t, []int{3, 2, 1}, []int{responses[0].ID, responses[1].ID, responses[2].ID})
	})

	t.Run("a batch can be partly successful", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Methods = map[string]Method{"eth_call": {RPCErrorRate: 1, RPCErrorCode: -32000}}
		srv, stub := serve(t, cfg)

		_, body := postBatch(t, srv.URL,
			`[{"jsonrpc":"2.0","id":1,"method":"eth_call"},{"jsonrpc":"2.0","id":2,"method":"eth_blockNumber"}]`)

		assert.Contains(t, string(body), `"code":-32000`)
		assert.Contains(t, string(body), `"result"`)
		assert.EqualValues(t, 1, stub.Stats().ByMethod["eth_call"][OutcomeRPCError])
		assert.EqualValues(t, 1, stub.Stats().ByMethod["eth_blockNumber"][OutcomeOK])
	})

	// A node with a batch limit rejects the whole array with one error object,
	// not with an array of them.
	t.Run("a batch over the limit is refused as a whole", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Node.MaxBatchSize = 2
		srv, _ := serve(t, cfg)

		status, body := postBatch(t, srv.URL,
			`[{"jsonrpc":"2.0","id":1,"method":"eth_call"},{"jsonrpc":"2.0","id":2,"method":"eth_call"},{"jsonrpc":"2.0","id":3,"method":"eth_call"}]`)

		assert.Equal(t, http.StatusOK, status)
		assert.Equal(t, byte('{'), body[0], "a rejected batch answers with an object, not an array")
		assert.Contains(t, string(body), "exceeds the limit of 2")
	})

	t.Run("an empty batch is refused", func(t *testing.T) {
		srv, _ := serve(t, DefaultConfig())
		_, body := postBatch(t, srv.URL, `[]`)
		assert.Contains(t, string(body), "empty batch")
	})
}

// The stub's tally is what an engine's own figures are reconciled against, so
// a batch has to count in the unit the engine counts it in: one entry per call
// it carried, in the class that befell them. A served batch of two records two;
// an abandoned one records two timeouts, not one phantom request and not
// nothing.
func TestAnAbandonedBatchCountsEveryCallItCarried(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Default.Latency = &Latency{Kind: LatencyFixed, MS: 200}
	stub, err := New(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	response, keepOpen := stub.Answer(ctx,
		[]byte(`[{"jsonrpc":"2.0","id":1,"method":"eth_call"},{"jsonrpc":"2.0","id":2,"method":"eth_getLogs"}]`))

	assert.Nil(t, response, "the client went away mid-answer, so there is nothing to send")
	assert.True(t, keepOpen)

	stats := stub.Stats()
	assert.EqualValues(t, 2, stats.Total, "a batch of two counts as two, whatever befell it")
	assert.EqualValues(t, 1, stats.ByMethod["eth_call"][OutcomeTimeout])
	assert.EqualValues(t, 1, stats.ByMethod["eth_getLogs"][OutcomeTimeout])
	assert.Zero(t, stats.ByMethod["eth_call"][OutcomeOK], "nothing was answered")
}

// The same for the HTTP path, which had the same defect and has to keep the
// same rule: the two transports are reconciled against each other.
func TestAnAbandonedBatchCountsEveryCallItCarriedOverHTTP(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Default.Latency = &Latency{Kind: LatencyFixed, MS: 200}
	srv, stub := serve(t, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, strings.NewReader(
		`[{"jsonrpc":"2.0","id":1,"method":"eth_call"},{"jsonrpc":"2.0","id":2,"method":"eth_getLogs"}]`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	//nolint:bodyclose // the request is abandoned on purpose, so there is no body
	_, err = http.DefaultClient.Do(req)
	require.Error(t, err, "the client goes away before the node answers")

	// The handler notices its context is done on its own schedule.
	require.Eventually(t, func() bool { return stub.Stats().Total == 2 }, time.Second, 10*time.Millisecond,
		"a batch of two counts as two over HTTP as well: %v", stub.Stats())
	assert.EqualValues(t, 1, stub.Stats().ByMethod["eth_call"][OutcomeTimeout])
	assert.EqualValues(t, 1, stub.Stats().ByMethod["eth_getLogs"][OutcomeTimeout])
}

// A batch holds a serving slot for its whole round trip over a socket, as it
// does over HTTP. Without that the socket transports were the one way past the
// node's own concurrency limit.
func TestBatchesOverSocketsRespectTheConcurrencyLimit(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Node.ConcurrencyLimit = 1
	cfg.Default.Latency = &Latency{Kind: LatencyFixed, MS: 60}
	stub, err := New(cfg)
	require.NoError(t, err)

	batch := []byte(`[{"jsonrpc":"2.0","id":1,"method":"eth_call"},{"jsonrpc":"2.0","id":2,"method":"eth_call"}]`)

	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			response, _ := stub.Answer(context.Background(), batch)
			assert.NotNil(t, response)
		}()
	}
	wg.Wait()

	assert.GreaterOrEqual(t, time.Since(start), 120*time.Millisecond,
		"two batches were served at once by a node that serves one thing at a time")
	assert.EqualValues(t, 4, stub.Stats().Total)
}

// A batch refused as a whole is refused for every call it carried, and the two
// transports have to say so identically: an engine reconciled against this
// tally over a socket and over HTTP is reading the same node.
func TestAWholeBatchRefusalCountsEveryCallItCarried(t *testing.T) {
	batch := `[{"jsonrpc":"2.0","id":1,"method":"eth_call"},{"jsonrpc":"2.0","id":2,"method":"eth_getLogs"}]`

	config := func() Config {
		cfg := DefaultConfig()
		cfg.Node.MaxBatchSize = 1
		return cfg
	}

	t.Run("over a socket", func(t *testing.T) {
		stub, err := New(config())
		require.NoError(t, err)

		response, keepOpen := stub.Answer(context.Background(), []byte(batch))
		require.NotNil(t, response)
		assert.True(t, keepOpen)
		assert.Contains(t, string(response), `"code":-32600`)

		assertRefusedBatchTally(t, stub.Stats())
	})

	t.Run("over HTTP", func(t *testing.T) {
		srv, stub := serve(t, config())

		resp, err := http.Post(srv.URL, "application/json", strings.NewReader(batch))
		require.NoError(t, err)
		defer resp.Body.Close()
		out, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Contains(t, string(out), `"code":-32600`)

		assertRefusedBatchTally(t, stub.Stats())
	})
}

// The tally a refused batch of two must leave, whichever transport carried it:
// one entry per call, against the method that call asked for, in the class the
// caller reads off a -32600.
func assertRefusedBatchTally(t *testing.T, stats Stats) {
	t.Helper()
	assert.EqualValues(t, 2, stats.Total, "a refusal of a batch of two is two calls refused")
	assert.EqualValues(t, 1, stats.ByMethod["eth_call"][OutcomeRPCError])
	assert.EqualValues(t, 1, stats.ByMethod["eth_getLogs"][OutcomeRPCError])
	assert.NotContains(t, stats.ByMethod, "batch", "no caller asked for a method called batch")
	assert.NotContains(t, stats.ByMethod, "", "every call in a refused batch names its own method")
}
