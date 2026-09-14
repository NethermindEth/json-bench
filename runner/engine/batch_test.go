package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/internal/stubnode"
)

func requestsFor(methods ...string) []Request {
	out := make([]Request, 0, len(methods))
	for i, method := range methods {
		id := i + 1
		out = append(out, Request{
			ID:      id,
			Name:    method,
			Method:  method,
			Payload: []byte(fmt.Sprintf(`{"id":%d,"jsonrpc":"2.0","method":%q,"params":[]}`, id, method)),
		})
	}
	return out
}

func TestBuildBatches(t *testing.T) {
	requests := requestsFor("a", "b", "c", "d", "e")

	t.Run("a size of one leaves the payload untouched", func(t *testing.T) {
		batches, err := BuildBatches(requests, 1)
		require.NoError(t, err)
		require.Len(t, batches, 5)
		for i, batch := range batches {
			assert.False(t, batch.Batched())
			assert.Equal(t, requests[i].Payload, batch.Payload,
				"an unbatched run must send exactly what it sent before batching existed")
		}
	})

	t.Run("groups into arrays", func(t *testing.T) {
		batches, err := BuildBatches(requests, 2)
		require.NoError(t, err)
		require.Len(t, batches, 3)

		assert.Equal(t, 2, batches[0].Size())
		assert.Equal(t, 1, batches[2].Size(), "the last batch carries the remainder")
		assert.True(t, batches[0].Batched())
		assert.False(t, batches[2].Batched(), "a batch of one is a plain call")

		var members []map[string]any
		require.NoError(t, json.Unmarshal(batches[0].Payload, &members))
		require.Len(t, members, 2)
		assert.Equal(t, "a", members[0]["method"])
		assert.Equal(t, "b", members[1]["method"])
	})

	// The members' payloads are spliced in verbatim, so a batched run sends
	// byte-identical call objects to an unbatched one.
	t.Run("members are not re-encoded", func(t *testing.T) {
		batches, err := BuildBatches(requests[:2], 2)
		require.NoError(t, err)
		assert.Equal(t,
			`[`+string(requests[0].Payload)+`,`+string(requests[1].Payload)+`]`,
			string(batches[0].Payload))
	})

	t.Run("a size beyond the sequence is one batch", func(t *testing.T) {
		batches, err := BuildBatches(requests, 100)
		require.NoError(t, err)
		require.Len(t, batches, 1)
		assert.Equal(t, 5, batches[0].Size())
	})

	t.Run("a malformed member is refused", func(t *testing.T) {
		_, err := BuildBatches([]Request{{ID: 1, Payload: []byte(`{not json`)}}, 2)
		assert.Error(t, err)
	})
}

func TestClassifyBatch(t *testing.T) {
	batch, err := BuildBatches(requestsFor("eth_call", "eth_getLogs", "eth_blockNumber"), 3)
	require.NoError(t, err)
	require.Len(t, batch, 1)
	subject := batch[0]

	outcomesOf := func(results []batchOutcome) []Outcome {
		out := make([]Outcome, 0, len(results))
		for _, r := range results {
			out = append(out, r.outcome)
		}
		return out
	}

	t.Run("each member is matched by id, not by position", func(t *testing.T) {
		// Answered in reverse, with the middle call failing.
		body := `[
			{"jsonrpc":"2.0","id":3,"result":"0x1"},
			{"jsonrpc":"2.0","id":2,"error":{"code":-32000,"message":"reverted"}},
			{"jsonrpc":"2.0","id":1,"result":"0xabc"}
		]`
		results := classifyBatch(subject, 200, []byte(body), nil)

		require.Len(t, results, 3)
		assert.Equal(t, []Outcome{OutcomeOK, OutcomeRPCError, OutcomeOK}, outcomesOf(results))
		assert.Equal(t, -32000, results[1].rpcCode, "the failing call's own code, attributed to it alone")
	})

	t.Run("a null result inside a batch is still a null result", func(t *testing.T) {
		body := `[{"jsonrpc":"2.0","id":1,"result":null},{"jsonrpc":"2.0","id":2,"result":[]},{"jsonrpc":"2.0","id":3,"result":"0x1"}]`
		results := classifyBatch(subject, 200, []byte(body), nil)
		assert.Equal(t, []Outcome{OutcomeRPCNull, OutcomeRPCNull, OutcomeOK}, outcomesOf(results))
	})

	// This is what exceeding a node's max batch size looks like: one error
	// object instead of an array. It belongs to every call in the batch.
	t.Run("a batch refused as a whole fails every member", func(t *testing.T) {
		body := `{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"batch too large"}}`
		results := classifyBatch(subject, 200, []byte(body), nil)

		assert.Equal(t, []Outcome{OutcomeRPCError, OutcomeRPCError, OutcomeRPCError}, outcomesOf(results))
		for _, r := range results {
			assert.Equal(t, -32600, r.rpcCode)
		}
	})

	t.Run("an HTTP failure fails every member", func(t *testing.T) {
		results := classifyBatch(subject, 503, []byte("unavailable"), nil)
		assert.Equal(t, []Outcome{OutcomeHTTPError, OutcomeHTTPError, OutcomeHTTPError}, outcomesOf(results))
	})

	t.Run("a transport failure fails every member", func(t *testing.T) {
		results := classifyBatch(subject, 0, nil, context.DeadlineExceeded)
		assert.Equal(t, []Outcome{OutcomeTimeout, OutcomeTimeout, OutcomeTimeout}, outcomesOf(results))
	})

	t.Run("a member the node did not answer is not a success", func(t *testing.T) {
		body := `[{"jsonrpc":"2.0","id":1,"result":"0x1"},{"jsonrpc":"2.0","id":3,"result":"0x1"}]`
		results := classifyBatch(subject, 200, []byte(body), nil)
		assert.Equal(t, []Outcome{OutcomeOK, OutcomeTruncated, OutcomeOK}, outcomesOf(results))
	})

	t.Run("an unparseable array fails every member", func(t *testing.T) {
		results := classifyBatch(subject, 200, []byte(`[{"id":1,"result"`), nil)
		assert.Equal(t, []Outcome{OutcomeTruncated, OutcomeTruncated, OutcomeTruncated}, outcomesOf(results))
	})

	// A lone success where an array was asked for is not an answer to anything
	// in particular, so it is not counted as three successes.
	t.Run("a single success where an array was expected answers nothing", func(t *testing.T) {
		results := classifyBatch(subject, 200, []byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`), nil)
		assert.Equal(t, []Outcome{OutcomeTruncated, OutcomeTruncated, OutcomeTruncated}, outcomesOf(results))
	})
}

func batchRunConfig(url string, size int, mutate func(*config.Config)) *config.Config {
	return runConfig(url, []*config.Call{
		{Name: "eth_call", Method: "eth_call", Params: []any{}, Weight: 1},
	}, func(c *config.Config) {
		c.Duration = "2s"
		c.RPS = 100
		c.VUs = 30
		c.BatchSize = size
		if mutate != nil {
			mutate(c)
		}
	})
}

// rps stays a rate of requests, so a batched run offers the node the same work
// as an unbatched one in a tenth of the round trips. That is what makes two
// batch sizes comparable.
func TestBatchingHoldsTheRequestRateAndReducesRoundTrips(t *testing.T) {
	stubCfg := stubnode.DefaultConfig()
	stubCfg.Default.Latency = &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 2}
	srv := stubServer(t, stubCfg)

	result, _, err := Run(context.Background(), batchRunConfig(srv.URL, 10, nil), testOptions(t))
	require.NoError(t, err)

	client := result.ClientMetrics["stub"]
	// 100 rps for 2s is 200 requests, carried by 20 batches.
	assert.InDelta(t, 200, client.TotalRequests, 20)
	assert.EqualValues(t, 10, result.Manifest.BatchSize)

	delivery := result.Summary["delivery"].(map[string]any)["stub"].(map[string]any)
	assert.InDelta(t, 20, delivery["sent"].(int), 3, "one arrival per batch")
}

func TestBatchLatencyIsTheRoundTripEveryCallerWaited(t *testing.T) {
	stubCfg := stubnode.DefaultConfig()
	stubCfg.Default.Latency = &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 30}
	srv := stubServer(t, stubCfg)

	opts := testOptions(t)
	result, _, err := Run(context.Background(), batchRunConfig(srv.URL, 5, func(c *config.Config) {
		c.Duration = "2s"
		c.RPS = 50
	}), opts)
	require.NoError(t, err)

	client := result.ClientMetrics["stub"]
	// Every member of a batch waited the whole round trip, so each records the
	// batch's latency rather than a share of it.
	assert.InDelta(t, 30, client.Latency.P50, 25)

	samples := readSampleFile(t, opts.OutputDir)
	byBatch := map[float64][]Sample{}
	for _, s := range samples {
		require.EqualValues(t, 5, s.BatchSize)
		byBatch[float64(s.Start.UnixNano())] = append(byBatch[float64(s.Start.UnixNano())], s)
	}

	var checked int
	for _, members := range byBatch {
		if len(members) < 2 {
			continue
		}
		checked++
		for _, member := range members[1:] {
			assert.Equal(t, members[0].Service(), member.Service(),
				"members of one round trip share its latency")
		}
	}
	assert.NotZero(t, checked)
}

// A batch is one iteration and many requests, which is the distinction the
// iteration family exists to carry.
func TestBatchingSeparatesIterationsFromRequests(t *testing.T) {
	accum := NewAccumulator()
	batch := []Sample{
		{Client: "c", Method: "eth_call", Name: "eth_call", Status: 200, Outcome: OutcomeOK,
			BatchSize: 3, BatchLeader: true, Phases: Phases{Waiting: 10_000_000}},
		{Client: "c", Method: "eth_call", Name: "eth_call", Status: 200, Outcome: OutcomeOK,
			BatchSize: 3, Phases: Phases{Waiting: 10_000_000}},
		{Client: "c", Method: "eth_call", Name: "eth_call", Status: 200, Outcome: OutcomeOK,
			BatchSize: 3, Phases: Phases{Waiting: 10_000_000}},
	}
	for _, s := range batch {
		accum.Add(s)
	}

	snap := accum.Snapshot("t", time.Now(), nil, nil, nil)
	require.Len(t, snap.Clients, 1)

	assert.EqualValues(t, 3, snap.Clients[0].Count, "three JSON-RPC calls")
	assert.EqualValues(t, 1, snap.Clients[0].Iterations, "one HTTP round trip")
	assert.EqualValues(t, 1, snap.Clients[0].Duration.Count,
		"the round trip's latency is recorded once, not once per member")
}

// A node that refuses batches over a limit must show up as an error rate, not
// as a fast run.
func TestBatchOverTheNodesLimitIsReportedAsErrors(t *testing.T) {
	stubCfg := stubnode.DefaultConfig()
	stubCfg.Default.Latency = &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 1}
	stubCfg.Node.MaxBatchSize = 3
	srv := stubServer(t, stubCfg)

	result, _, err := Run(context.Background(), batchRunConfig(srv.URL, 10, func(c *config.Config) {
		c.Duration = "1s"
		c.RPS = 100
	}), testOptions(t))
	require.NoError(t, err)

	client := result.ClientMetrics["stub"]
	require.NotZero(t, client.TotalRequests)

	assert.EqualValues(t, client.TotalRequests, client.TotalErrors,
		"every call in every rejected batch failed")
	assert.InDelta(t, 100, client.ErrorRate, 0.001)
	assert.EqualValues(t, client.TotalRequests, client.Outcomes["rpc_error"])
	assert.NotZero(t, client.ErrorTypes["rpc_code_-32600"], "the node's own rejection code")
}
