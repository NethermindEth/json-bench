package engine

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
