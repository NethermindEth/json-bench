package engine

import (
	"context"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/internal/stubnode"
)

func TestTransportKindFor(t *testing.T) {
	cases := map[string]TransportKind{
		"http://127.0.0.1:8545":  TransportHTTP,
		"https://node.example":   TransportHTTP,
		"ws://127.0.0.1:8546":    TransportWebSocket,
		"wss://node.example/rpc": TransportWebSocket,
		"ipc:///run/node.ipc":    TransportIPC,
		"unix:///run/node.ipc":   TransportIPC,
		"/run/node.ipc":          TransportIPC,
		"./node.ipc":             TransportIPC,
	}
	for raw, want := range cases {
		got, err := TransportKindFor(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, want, got, raw)
	}

	for _, raw := range []string{"", "node.example:8545", "ftp://node"} {
		_, err := TransportKindFor(raw)
		assert.Error(t, err, "%q should be rejected rather than assumed to be HTTP", raw)
	}
}

func TestIPCPathAcceptsBothForms(t *testing.T) {
	assert.Equal(t, "/run/node.ipc", ipcPath("ipc:///run/node.ipc"))
	assert.Equal(t, "/run/node.ipc", ipcPath("unix:///run/node.ipc"))
	assert.Equal(t, "/run/node.ipc", ipcPath("/run/node.ipc"))
}

// socketStub serves the same node over HTTP, WebSocket and IPC.
func socketStub(t *testing.T, cfg stubnode.Config) (httpURL, wsURL, ipcURL string, stub *stubnode.Stub) {
	t.Helper()
	var err error
	stub, err = stubnode.New(cfg)
	require.NoError(t, err)

	srv := httptest.NewServer(stub.Handler())
	t.Cleanup(srv.Close)

	// A Unix socket path is limited to about 104 bytes, and Go's per-test temp
	// directories are already most of that.
	dir, err := os.MkdirTemp("", "ipc")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "n.ipc")
	listener, err := stub.ListenIPC(sock)
	require.NoError(t, err)
	t.Cleanup(func() { listener.Close() })

	return srv.URL, "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws", "ipc://" + sock, stub
}

func socketRunConfig(url string) *config.Config {
	return runConfig(url, []*config.Call{
		{Name: "eth_call", Method: "eth_call", Params: []any{}, Weight: 1},
	}, func(c *config.Config) {
		c.Duration = "2s"
		c.RPS = 150
		c.VUs = 20
	})
}

// The same node, the same load, over each transport it offers.
func TestEveryTransportCarriesTheSameLoad(t *testing.T) {
	cfg := stubnode.DefaultConfig()
	cfg.Default.Latency = &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 2}
	httpURL, wsURL, ipcURL, _ := socketStub(t, cfg)

	for name, url := range map[string]string{"http": httpURL, "websocket": wsURL, "ipc": ipcURL} {
		t.Run(name, func(t *testing.T) {
			result, _, err := Run(context.Background(), socketRunConfig(url), testOptions(t))
			require.NoError(t, err)

			client := result.ClientMetrics["stub"]
			assert.InDelta(t, 300, client.TotalRequests, 30)
			assert.Zero(t, client.TotalErrors)
			assert.Equal(t, name, result.Manifest.Transports["stub"],
				"the manifest records how the target was reached, because the transports are not "+
					"the same measurement")
		})
	}
}

// Preflight identifies the node over whichever transport the config names, so a
// socket run has the same provenance an HTTP one does.
func TestPreflightIdentifiesTheTargetOverSockets(t *testing.T) {
	cfg := stubnode.DefaultConfig()
	cfg.Node.ClientVersion = "Nethermind/v1.99.0"
	_, wsURL, ipcURL, _ := socketStub(t, cfg)

	for name, url := range map[string]string{"websocket": wsURL, "ipc": ipcURL} {
		t.Run(name, func(t *testing.T) {
			runCfg := socketRunConfig(url)
			runCfg.Duration = "1s"
			result, _, err := Run(context.Background(), runCfg, testOptions(t))
			require.NoError(t, err)

			require.Len(t, result.Manifest.Clients, 1)
			assert.Equal(t, "Nethermind/v1.99.0", result.Manifest.Clients[0].ClientVersion)
			assert.Equal(t, "1", result.Manifest.Clients[0].ChainID)
		})
	}
}

// A multiplexed connection carries many requests at once and a node may answer
// in any order, so responses are matched by JSON-RPC id rather than by arrival.
func TestMultiplexedResponsesAreMatchedByID(t *testing.T) {
	m := newMux()

	first, err := m.register([]string{"1"})
	require.NoError(t, err)
	second, err := m.register([]string{"2"})
	require.NoError(t, err)

	// Answered out of order, which the spec permits.
	m.dispatch([]byte(`{"jsonrpc":"2.0","id":2,"result":"second"}`))
	m.dispatch([]byte(`{"jsonrpc":"2.0","id":1,"result":"first"}`))

	assert.Contains(t, string(<-second.done), "second")
	assert.Contains(t, string(<-first.done), "first")
}

// A batch's answer is an array with no id of its own, so it is matched by the
// ids of its members — and the stub answers those in reverse on purpose.
func TestBatchResponseIsMatchedByItsMembers(t *testing.T) {
	m := newMux()
	p, err := m.register(payloadIDs([]byte(`[{"id":1,"method":"a"},{"id":2,"method":"b"}]`)))
	require.NoError(t, err)

	m.dispatch([]byte(`[{"jsonrpc":"2.0","id":2,"result":"b"},{"jsonrpc":"2.0","id":1,"result":"a"}]`))

	select {
	case frame := <-p.done:
		assert.Contains(t, string(frame), `"id":1`)
	case <-time.After(time.Second):
		t.Fatal("the batch's answer was never matched to it")
	}
}

// Two in-flight requests sharing an id cannot both be answered, and matching one
// to the other's response would fabricate a measurement.
func TestMuxRefusesADuplicateInFlightID(t *testing.T) {
	m := newMux()
	_, err := m.register([]string{"7"})
	require.NoError(t, err)

	_, err = m.register([]string{"7"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already in flight")
}

// A payload with no id could never have its answer matched, so it is refused
// rather than left waiting for a response that cannot be recognised.
func TestMuxRefusesAPayloadWithoutAnID(t *testing.T) {
	m := newMux()
	_, err := m.register(payloadIDs([]byte(`{"jsonrpc":"2.0","method":"eth_call"}`)))
	require.Error(t, err)
}

// When the connection goes, every request waiting on it has failed.
func TestConnectionLossFailsEveryPendingRequest(t *testing.T) {
	m := newMux()
	first, err := m.register([]string{"1"})
	require.NoError(t, err)
	second, err := m.register([]string{"2"})
	require.NoError(t, err)

	m.fail(errConnectionClosed)

	assert.Nil(t, <-first.done)
	assert.Nil(t, <-second.done)

	_, err = m.register([]string{"3"})
	assert.Error(t, err, "a closed connection takes no new requests until it is re-established")
}

// A node pushing subscription notifications down the same socket is answering
// nothing that was measured, so those frames are dropped rather than handed to
// whichever request happens to be waiting.
func TestUnmatchedFramesAreIgnored(t *testing.T) {
	m := newMux()
	p, err := m.register([]string{"1"})
	require.NoError(t, err)

	m.dispatch([]byte(`{"jsonrpc":"2.0","method":"eth_subscription","params":{"subscription":"0xabc"}}`))
	m.dispatch([]byte(`{"jsonrpc":"2.0","id":99,"result":"not mine"}`))

	select {
	case <-p.done:
		t.Fatal("a frame that answered nothing was delivered to a waiting request")
	case <-time.After(50 * time.Millisecond):
	}

	m.dispatch([]byte(`{"jsonrpc":"2.0","id":1,"result":"mine"}`))
	assert.Contains(t, string(<-p.done), "mine")
}

// Batching works the same over a socket; the run just carries the array.
func TestBatchingOverSockets(t *testing.T) {
	cfg := stubnode.DefaultConfig()
	cfg.Default.Latency = &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 1}
	_, wsURL, ipcURL, _ := socketStub(t, cfg)

	for name, url := range map[string]string{"websocket": wsURL, "ipc": ipcURL} {
		t.Run(name, func(t *testing.T) {
			runCfg := socketRunConfig(url)
			runCfg.Duration = "2s"
			runCfg.RPS = 100
			runCfg.BatchSize = 5

			result, _, err := Run(context.Background(), runCfg, testOptions(t))
			require.NoError(t, err)

			client := result.ClientMetrics["stub"]
			assert.InDelta(t, 200, client.TotalRequests, 30, "rps stays a rate of requests")
			assert.Zero(t, client.TotalErrors)
		})
	}
}

// JSON-RPC errors are a property of the protocol, not of HTTP, so they are
// reported identically over a socket.
func TestRPCErrorsAreClassifiedOverSockets(t *testing.T) {
	cfg := stubnode.DefaultConfig()
	cfg.Default.Latency = &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 1}
	cfg.Methods = map[string]stubnode.Method{
		"eth_call": {RPCErrorRate: 1, RPCErrorCode: -32000},
	}
	_, wsURL, ipcURL, _ := socketStub(t, cfg)

	for name, url := range map[string]string{"websocket": wsURL, "ipc": ipcURL} {
		t.Run(name, func(t *testing.T) {
			runCfg := socketRunConfig(url)
			runCfg.Duration = "1s"
			runCfg.RPS = 60

			result, _, err := Run(context.Background(), runCfg, testOptions(t))
			require.NoError(t, err)

			client := result.ClientMetrics["stub"]
			require.NotZero(t, client.TotalRequests)
			assert.EqualValues(t, client.TotalRequests, client.Outcomes["rpc_error"])
			assert.NotZero(t, client.ErrorTypes["rpc_code_-32000"])
		})
	}
}

// A rejected registration must not disturb the request it collided with.
func TestRejectedRegistrationLeavesTheVictimIntact(t *testing.T) {
	m := newMux()

	victim, err := m.register([]string{"1"})
	require.NoError(t, err)

	// A batch whose second member collides with the in-flight id above.
	_, err = m.register([]string{"2", "1"})
	require.Error(t, err, "the colliding batch must be refused")

	// The victim is still in flight and must still receive its answer.
	m.dispatch([]byte(`{"jsonrpc":"2.0","id":1,"result":"mine"}`))
	select {
	case frame := <-victim.done:
		assert.Contains(t, string(frame), "mine")
	case <-time.After(200 * time.Millisecond):
		t.Fatal("the in-flight request was orphaned by the rejected one and will now time out")
	}

	// And the refused request's own id must not have been left claimed.
	_, err = m.register([]string{"2"})
	assert.NoError(t, err, "id 2 was never successfully claimed, so it must be free")
}

// A reconnect must get a mux of its own. The reader of the socket that died may
// still be blocked in a read when the replacement is already carrying requests,
// and when it finally wakes it reports the failure it saw — which must reach
// its own generation's requests and nothing else.
func TestReconnectDoesNotInheritTheDeadConnectionsMux(t *testing.T) {
	_, wsURL, _, _ := socketStub(t, stubnode.DefaultConfig())

	w := newWebSocketConn(wsURL, nil, 5*time.Second, TransportOptions{})
	t.Cleanup(func() { _ = w.Close() })

	first, stale, _, err := w.connect(context.Background())
	require.NoError(t, err)

	// What a failed write does: forget the socket so the next request dials.
	w.dropIfCurrent(first)

	second, live, _, err := w.connect(context.Background())
	require.NoError(t, err)
	require.NotSame(t, first, second, "a dropped socket must not be handed out again")
	require.NotSame(t, stale, live, "the new connection must match responses on its own state")

	p, err := live.register([]string{"1"})
	require.NoError(t, err)

	stale.fail(errConnectionClosed)

	live.dispatch([]byte(`{"jsonrpc":"2.0","id":1,"result":"answered"}`))
	select {
	case frame := <-p.done:
		require.NotNil(t, frame, "the live connection was failed by the dead one's reader")
		assert.Contains(t, string(frame), "answered")
	case <-time.After(time.Second):
		t.Fatal("the request on the live connection was never answered")
	}

	_, err = live.register([]string{"2"})
	assert.NoError(t, err, "the live connection must still take requests")
}

// blackhole accepts connections and never answers, which is how a node that is
// listening but not serving behaves: the handshake runs to its timeout.
func blackhole(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	var mu sync.Mutex
	var held []net.Conn
	t.Cleanup(func() {
		listener.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range held {
			conn.Close()
		}
	})

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			held = append(held, conn)
			mu.Unlock()
		}
	}()

	return "ws://" + listener.Addr().String()
}

// Requests arriving while a handshake is in flight wait on its result, not on a
// mutex. Queueing on the mutex made one unreachable target cost a multiple of
// the handshake timeout: every waiting request paid for a fresh dial of its
// own, one after another, and no request deadline could cut that short.
func TestADeadTargetDoesNotSerialiseItsDials(t *testing.T) {
	const handshakeTimeout = 400 * time.Millisecond
	const vus = 8

	w := newWebSocketConn(blackhole(t), nil, handshakeTimeout, TransportOptions{})
	t.Cleanup(func() { _ = w.Close() })

	errs := make(chan error, vus)
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < vus; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _, err := w.connect(context.Background())
			errs <- err
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	for i := 0; i < vus; i++ {
		assert.Error(t, <-errs, "a node that never completes the handshake cannot be reached")
	}
	assert.Less(t, elapsed, vus/2*handshakeTimeout,
		"the dials serialised: %d requests took %s against a handshake timeout of %s",
		vus, elapsed, handshakeTimeout)
}

// A request waiting on someone else's handshake still answers to its own
// deadline, which a mutex could not do.
func TestAWaitingRequestKeepsItsOwnDeadlineDuringADial(t *testing.T) {
	w := newWebSocketConn(blackhole(t), nil, 10*time.Second, TransportOptions{})
	t.Cleanup(func() { _ = w.Close() })

	go func() { _, _, _, _ = w.connect(context.Background()) }()

	// Long enough that the goroutine above is the one dialling.
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, _, _, err := w.connect(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 2*time.Second,
		"the request waited out the handshake instead of its own deadline")
}

// A refusal must never be charged to a request the node answered. When the
// requests in flight are not alike, the frame cannot be told from one belonging
// to any of them, and guessing would record a failure against a call that
// succeeded, discard that call's real answer, and still leave the refused call
// to time out: one refusal becoming two failures and a lost success.
func TestARefusalIsDroppedRatherThanChargedToADifferentShape(t *testing.T) {
	m := newMux()

	batch, err := m.register([]string{"1", "2"})
	require.NoError(t, err)
	single, err := m.register([]string{"3"})
	require.NoError(t, err)

	m.dispatch([]byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"batch too large"}}`))

	select {
	case <-single.done:
		t.Fatal("the refusal was charged to a request of a different shape")
	case <-batch.done:
		t.Fatal("the refusal was charged to a request that may not have caused it")
	case <-time.After(50 * time.Millisecond):
	}

	// Both are still in flight, so both still receive their own answers. The
	// success that the guess would have discarded is the reason for the rule.
	m.dispatch([]byte(`{"jsonrpc":"2.0","id":3,"result":"mine"}`))
	assert.Contains(t, string(<-single.done), "mine")

	m.dispatch([]byte(`[{"jsonrpc":"2.0","id":1,"result":"a"},{"jsonrpc":"2.0","id":2,"result":"b"}]`))
	assert.Contains(t, string(<-batch.done), `"id":1`)
}

// A node that rejects a request before reading its id answers with "id": null.
// There is nothing to match on, so while every request in flight is alike — the
// same batch size, refused alike — the frame goes to the one that has been
// waiting longest, reporting the node's own error rather than a timeout for an
// answer that did arrive.
func TestNullIDErrorIsDeliveredRatherThanDropped(t *testing.T) {
	m := newMux()
	first, err := m.register([]string{"1"})
	require.NoError(t, err)
	second, err := m.register([]string{"2"})
	require.NoError(t, err)

	m.dispatch([]byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"batch too large"}}`))

	select {
	case frame := <-first.done:
		assert.Contains(t, string(frame), "-32600")
	case <-time.After(time.Second):
		t.Fatal("the node's error was dropped and the request will now time out")
	}

	// The request that was not given it is still in flight and still matched.
	m.dispatch([]byte(`{"jsonrpc":"2.0","id":2,"result":"mine"}`))
	assert.Contains(t, string(<-second.done), "mine")
}

// A frame carrying no id that is not one of those two refusals is not a
// response to a request: a notification, or the connection-level errors geth
// and erigon push down the socket. Handing one to a waiting request would fail
// a call the node never refused, and leave the real culprit to time out — one
// failure recorded as two, against the wrong call.
func TestUnattributableFramesThatAnswerNothingAreStillDropped(t *testing.T) {
	for name, frame := range map[string]string{
		"a notification":             `{"jsonrpc":"2.0","method":"eth_subscription","params":{"subscription":"0xabc"}}`,
		"a null result":              `{"jsonrpc":"2.0","id":null,"result":null}`,
		"an internal error":          `{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"internal error"}}`,
		"a write timeout":            `{"jsonrpc":"2.0","id":null,"error":{"code":-32000,"message":"write timeout"}}`,
		"a connection-level message": `{"jsonrpc":"2.0","id":null,"error":{"code":-32001,"message":"message too large"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			m := newMux()
			p, err := m.register([]string{"1"})
			require.NoError(t, err)

			m.dispatch([]byte(frame))

			select {
			case <-p.done:
				t.Fatal("a frame that answered nothing was delivered to a waiting request")
			case <-time.After(50 * time.Millisecond):
			}
		})
	}
}

// A batch over the node's own limit is refused with one error object and a null
// id, which is the shape that has to survive the whole way to the report.
func TestABatchOverTheNodesLimitIsReportedAsTheErrorItIs(t *testing.T) {
	const batchSize = 5

	for _, transport := range []string{"websocket", "ipc"} {
		t.Run(transport, func(t *testing.T) {
			cfg := stubnode.DefaultConfig()
			cfg.Default.Latency = &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 1}
			cfg.Node.MaxBatchSize = 2

			// A stub of its own per transport, so its tally describes this run
			// and can be held against what the client reported.
			_, wsURL, ipcURL, stub := socketStub(t, cfg)
			url := wsURL
			if transport == "ipc" {
				url = ipcURL
			}

			runCfg := socketRunConfig(url)
			runCfg.Duration = "1s"
			runCfg.RPS = 60
			runCfg.BatchSize = batchSize

			result, _, err := Run(context.Background(), runCfg, testOptions(t))
			require.NoError(t, err)

			client := result.ClientMetrics["stub"]
			require.NotZero(t, client.TotalRequests)
			assert.NotZero(t, client.ErrorTypes["rpc_code_-32600"],
				"the node's batch-limit error was recorded as something else: %v", client.ErrorTypes)
			assert.Zero(t, client.Outcomes[string(OutcomeTimeout)],
				"a refused batch is an answer, not a timeout")

			// Both sides count a whole-batch refusal in calls, so they agree on
			// how many were refused. The tolerance is one batch: the run can
			// end with one in flight, which the node has refused and the client
			// has not yet read.
			refusedByTheNode := stub.Stats().ByMethod["eth_call"][stubnode.OutcomeRPCError]
			assert.InDelta(t, refusedByTheNode, client.ErrorTypes["rpc_code_-32600"], batchSize,
				"the node refused %d calls and the client recorded %d",
				refusedByTheNode, client.ErrorTypes["rpc_code_-32600"])
		})
	}
}
