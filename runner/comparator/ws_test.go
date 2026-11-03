package comparator

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/jsonrpc-bench/runner/types"
)

const subscriptionScenarioLine = `{"scenario":"subscription","transport":"ws","steps":[` +
	`{"method":"eth_subscribe","params":["newHeads"]},` +
	`{"method":"eth_unsubscribe","params":[{"$ref":0}],"wait_ms":50},` +
	`{"method":"eth_unsubscribe","params":[{"$ref":0}]}]}`

const subscriptionInvalidLine = `{"scenario":"subscription_invalid","transport":"ws","steps":[` +
	`{"method":"eth_subscribe","params":["fooBar"]}]}`

// The identities below were produced by rpc-corpus-tools (canonical.py,
// scenario_id and scenario_step_id) for subscriptionScenarioLine: a scenario
// with a transport hashes ["scenario/ws", [{method, params[, wait_ms]}, ...]].
const subscriptionScenarioID = "38757a5644f10fa2b91935ac746c32ae8139a6d888c59f4395f4029921973900"

var subscriptionScenarioStepIDs = []string{
	"2be060c2634d87212c87d45f59a21a7c0b5d5138892bd6521b40e2b0038c779a",
	"37a59ff4a6a134b76eb866b7302dd7cfb75238e389a885b4b42ca865cb2948c3",
	"5de2a7e8ed6e0f1503427c537c1546fc0d3edf0c15924536f3ba5d8fb4abd644",
}

// wsNode is a fake node that serves subscriptions over WebSocket and refuses
// them over HTTP, as Nethermind does.
type wsNode struct {
	mu       sync.Mutex
	next     int
	notify   bool
	conns    int
	received []rpcRequest
	httpHits int
}

func newWSNode(t *testing.T, firstID int, notify bool) (*wsNode, *httptest.Server) {
	t.Helper()
	node := &wsNode{next: firstID, notify: notify}
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !websocket.IsWebSocketUpgrade(r) {
			node.mu.Lock()
			node.httpHits++
			node.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		node.mu.Lock()
		node.conns++
		node.mu.Unlock()
		subs := map[string]bool{}
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var req rpcRequest
			if err := json.Unmarshal(data, &req); err != nil {
				return
			}
			node.mu.Lock()
			node.received = append(node.received, req)
			resp := map[string]interface{}{"jsonrpc": "2.0", "id": req.ID}
			var note map[string]interface{}
			arg, _ := firstParam(req)
			switch req.Method {
			case "eth_subscribe":
				if arg != "newHeads" {
					resp["error"] = map[string]interface{}{"code": -32602, "message": "Invalid params"}
					break
				}
				sid := fmt.Sprintf("0x%032x", node.next)
				node.next++
				subs[sid] = true
				resp["result"] = sid
				if node.notify {
					note = map[string]interface{}{"jsonrpc": "2.0", "method": "eth_subscription",
						"params": map[string]interface{}{"subscription": sid, "result": map[string]interface{}{"number": "0x1"}}}
				}
			case "eth_unsubscribe":
				resp["result"] = subs[arg]
				delete(subs, arg)
			default:
				resp["result"] = nil
			}
			node.mu.Unlock()
			out, _ := json.Marshal(resp)
			if err := conn.WriteMessage(websocket.TextMessage, out); err != nil {
				return
			}
			if note != nil {
				out, _ := json.Marshal(note)
				_ = conn.WriteMessage(websocket.TextMessage, out)
			}
		}
	}))
	t.Cleanup(srv.Close)
	return node, srv
}

func TestScenarioIdentityWithTransportMatchesTheProducer(t *testing.T) {
	entry := decodeScenarioLine(t, subscriptionScenarioLine)
	if entry.Transport != ScenarioTransportWS || entry.Steps[1].WaitMs != 50 {
		t.Fatalf("decoded %+v", entry)
	}
	id, err := ScenarioIdentity(entry.Transport, entry.Steps)
	if err != nil {
		t.Fatal(err)
	}
	if id != subscriptionScenarioID {
		t.Fatalf("ScenarioIdentity = %s, want %s", id, subscriptionScenarioID)
	}
	for i, step := range entry.Steps {
		got, err := ScenarioStepID(id, i, step)
		if err != nil {
			t.Fatal(err)
		}
		if got != subscriptionScenarioStepIDs[i] {
			t.Errorf("step %d id = %s, want %s", i, got, subscriptionScenarioStepIDs[i])
		}
	}

	// The transport and the wait are part of the identity; without them the
	// identity is the HTTP one.
	http, _ := ScenarioID(entry.Steps)
	if http == id {
		t.Fatal("the same steps over HTTP and WebSocket are one scenario")
	}
	noWait := append([]ScenarioStep(nil), entry.Steps...)
	noWait[1].WaitMs = 0
	if other, _ := ScenarioIdentity(ScenarioTransportWS, noWait); other == id {
		t.Fatal("a different wait is the same scenario")
	}
}

func TestScenarioTransportValidation(t *testing.T) {
	for name, line := range map[string]string{
		"unknown transport": `{"scenario":"s","transport":"ipc","steps":[{"method":"eth_subscribe","params":["newHeads"]}]}`,
		"negative wait":     `{"scenario":"s","transport":"ws","steps":[{"method":"eth_subscribe","params":[],"wait_ms":-1}]}`,
		"wait too long":     `{"scenario":"s","transport":"ws","steps":[{"method":"eth_subscribe","params":[],"wait_ms":60001}]}`,
		"wait overflows":    `{"scenario":"s","transport":"ws","steps":[{"method":"eth_subscribe","params":[],"wait_ms":9300000000000}]}`,
		"transport on call": `{"method":"eth_blockNumber","params":[],"transport":"ws"}`,
	} {
		dir := writeCorpus(t, map[string]string{"s.jsonl": line + "\n"})
		if _, err := readCorpusFile(dir, dir+"/s.jsonl"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestWSURL(t *testing.T) {
	for in, want := range map[string]string{
		"http://127.0.0.1:18545":     "ws://127.0.0.1:18545",
		"https://node.example/rpc/x": "wss://node.example/rpc/x",
		"ws://127.0.0.1:1":           "ws://127.0.0.1:1",
	} {
		got, err := wsURL(&types.ClientConfig{Name: "n", URL: in})
		if err != nil || got != want {
			t.Errorf("%s: %s, %v", in, got, err)
		}
	}
	got, _ := wsURL(&types.ClientConfig{Name: "n", URL: "http://a:1", WSURL: "ws://b:2"})
	if got != "ws://b:2" {
		t.Errorf("ws_url not used: %s", got)
	}
	if _, err := wsURL(&types.ClientConfig{Name: "n", URL: "ipc:///x"}); err == nil {
		t.Error("an ipc url has a WebSocket counterpart")
	}
}

func TestWSScenarioRunsOnOneConnectionPerNode(t *testing.T) {
	a, srvA := newWSNode(t, 1, false)
	b, srvB := newWSNode(t, 0x100, false)
	var lines []string
	for i := 0; i < 3; i++ {
		lines = append(lines, strings.Replace(subscriptionScenarioLine, `"wait_ms":50`, fmt.Sprintf(`"wait_ms":%d`, 50+i), 1))
	}
	lines = append(lines, subscriptionInvalidLine)
	start := time.Now()
	comp := runScenarioCorpus(t, lines, 4, srvA, srvB)
	if time.Since(start) < 50*time.Millisecond {
		t.Fatal("the wait was not honoured")
	}

	results := comp.GetResults()
	if len(results) != 3*3+1 {
		t.Fatalf("%d results", len(results))
	}
	if s := comp.Summarize(); s.Identical != len(results) {
		for _, r := range results {
			t.Logf("%s: diff %v transport %v", r.Method, r.Differences, r.TransportErrors)
		}
		t.Fatalf("summary %+v", s)
	}
	byCall := resultsByCall(comp)
	sub := byCall["scenario:subscription_variant1#0"]
	if sub.Responses["node0"].(map[string]interface{})["result"] == sub.Responses["node1"].(map[string]interface{})["result"] {
		t.Fatal("the two nodes answered the same id; the test proves nothing")
	}
	first := byCall["scenario:subscription_variant1#1"].Responses["node0"].(map[string]interface{})["result"]
	again := byCall["scenario:subscription_variant1#2"].Responses["node0"].(map[string]interface{})["result"]
	if first != true || again != false {
		t.Fatalf("unsubscribe answered %v then %v: the steps did not share a connection", first, again)
	}
	if md := byCall["scenario:subscription_variant1#1"].Metadata; md["transport"] != "ws" {
		t.Fatalf("metadata %v", md)
	}
	for _, node := range []*wsNode{a, b} {
		// Four scenarios, one connection each; the comparator's own HTTP
		// probes (chain id) are the only HTTP traffic.
		if node.conns != 4 {
			t.Fatalf("%d connections", node.conns)
		}
		for _, req := range node.received {
			if req.Method != "eth_subscribe" && req.Method != "eth_unsubscribe" {
				t.Fatalf("sent %s over WebSocket", req.Method)
			}
		}
	}
}

// A node that delivers a notification on a parked chain differs from one that
// does not, at the step after which it arrived, although every answer agrees.
func TestWSScenarioNotificationIsADifference(t *testing.T) {
	_, srvA := newWSNode(t, 1, false)
	_, srvB := newWSNode(t, 1, true)
	comp := runScenarioCorpus(t, []string{subscriptionScenarioLine}, 1, srvA, srvB)
	byCall := resultsByCall(comp)

	if r := byCall["scenario:subscription_variant1#0"]; len(r.Differences) != 0 {
		t.Fatalf("subscribe: %v", r.Differences)
	}
	r := byCall["scenario:subscription_variant1#1"]
	diff, _ := r.Differences["node1"].(map[string]interface{})
	if diff["notification_differences"] == nil {
		t.Fatalf("no notification difference: %+v", r.Differences)
	}
	counts := r.Metadata["notifications"].(map[string]interface{})
	if counts["node0"] != 0 || counts["node1"] != 1 {
		t.Fatalf("counts %v", counts)
	}
	if r := byCall["scenario:subscription_variant1#2"]; len(r.Differences) != 0 {
		t.Fatalf("a notification was counted twice: %v", r.Differences)
	}
}

// A node with no WebSocket endpoint fails every step closed.
func TestWSScenarioDialFailureIsATransportFailure(t *testing.T) {
	_, srvA := newWSNode(t, 1, false)
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))
	}))
	t.Cleanup(plain.Close)
	comp := runScenarioCorpus(t, []string{subscriptionScenarioLine}, 1, srvA, plain)
	results := resultsByCall(comp)
	if len(results) != 3 {
		t.Fatalf("%d results", len(results))
	}
	if cls := results["scenario:subscription_variant1#0"].TransportErrorClass["node1"]; cls != TransportWSDial {
		t.Fatalf("subscribe: %q", cls)
	}
	for step := 0; step < 3; step++ {
		r := results[fmt.Sprintf("scenario:subscription_variant1#%d", step)]
		if r.TransportErrorClass["node1"] == "" || len(r.Differences) != 0 {
			t.Fatalf("step %d: %+v %+v", step, r.TransportErrorClass, r.Differences)
		}
	}
	if !comp.HasTransportErrors() {
		t.Fatal("--fail-on-transport-error would not trip")
	}
}

// A client that did not answer the previous step has a count spanning two
// steps' windows; it is recorded, never compared, so it cannot pose as a
// node that pushed a notification.
func TestNotificationCountAfterAMissedStepIsNotCompared(t *testing.T) {
	newResult := func() *ComparisonResult {
		return &ComparisonResult{Metadata: map[string]interface{}{}, Differences: map[string]interface{}{}}
	}
	counts := map[string]int{"node0": 0, "node1": 2}

	r := newResult()
	noteNotifications(r, "node0", counts, map[string]bool{"node1": true})
	if len(r.Differences) != 0 {
		t.Fatalf("an uneven client was compared: %v", r.Differences)
	}
	if r.Metadata["notifications"].(map[string]interface{})["node1"] != 2 {
		t.Fatalf("its count was not recorded: %v", r.Metadata)
	}

	r = newResult()
	noteNotifications(r, "node0", counts, map[string]bool{"node0": true})
	if len(r.Differences) != 0 {
		t.Fatalf("compared against an uneven reference: %v", r.Differences)
	}

	r = newResult()
	noteNotifications(r, "node0", counts, nil)
	if r.Differences["node1"] == nil {
		t.Fatal("an even client with another count did not differ")
	}
}
