package comparator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/jsonrpc-bench/runner/types"
)

// A scenario whose line says "transport":"ws" runs every step on one WebSocket
// connection per client, opened when the scenario starts and closed when it
// ends. eth_subscribe and eth_unsubscribe need this: a subscription belongs to
// the connection that made it, so the steps of a subscription's lifecycle are
// only meaningful on one socket.
//
// Messages the node sends without being asked — eth_subscription
// notifications — are counted per connection. Each step records how many
// arrived on each client's connection since that client's previous answer, and
// two clients that received a different number before the same step differ,
// whatever each step answered. Their payloads are not compared: a notification
// names the subscription id the node chose.

// ScenarioTransportWS is the transport value of a WebSocket scenario.
const ScenarioTransportWS = "ws"

// Transport error classes of a WebSocket scenario step.
const (
	// TransportWSDial means the client's WebSocket endpoint could not be
	// opened, so none of the scenario's steps reached it.
	TransportWSDial = "ws_dial"
	// TransportWSClosed means the connection ended before the step was
	// answered.
	TransportWSClosed = "ws_closed"
)

// maxScenarioWait caps a step's wait_ms, so a corpus line cannot stall a run.
const maxScenarioWait = 60 * time.Second

// wsURL is the WebSocket endpoint of a client: its ws_url when set, else its
// url with the http scheme replaced by ws (https by wss).
func wsURL(client *types.ClientConfig) (string, error) {
	if client.WSURL != "" {
		return client.WSURL, nil
	}
	u, err := url.Parse(client.URL)
	if err != nil {
		return "", fmt.Errorf("client %s url: %w", client.Name, err)
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("client %s url %q has no WebSocket counterpart; set ws_url", client.Name, client.URL)
	}
	return u.String(), nil
}

// wsAnswer is one response, or the reason none came.
// notifications is how many notifications arrived on the connection after
// the previous answer and before this one, taken when the reader routes the
// answer, so one that arrives right after it is never counted early.
type wsAnswer struct {
	response      map[string]interface{}
	notifications int
	err           error
}

// wsConn is one client's connection for one scenario. A reader goroutine
// routes every response to the call waiting on its id and counts everything
// else as a notification.
type wsConn struct {
	conn         *websocket.Conn
	timeout      time.Duration
	exactNumbers bool

	writeMu sync.Mutex
	mu      sync.Mutex
	nextID  int
	waiting map[int]chan wsAnswer
	// notes counts the notifications received since the previous answer.
	notes  int
	closed error
	done   chan struct{}
}

func dialWS(cfg *ComparisonConfig, client *types.ClientConfig) (*wsConn, error) {
	endpoint, err := wsURL(client)
	if err != nil {
		return nil, &transportError{class: TransportWSDial, message: err.Error()}
	}
	header := http.Header{}
	for k, v := range client.Headers {
		header.Set(k, v)
	}
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	dialer := websocket.Dialer{HandshakeTimeout: timeout, Proxy: http.ProxyFromEnvironment}
	conn, resp, err := dialer.Dial(endpoint, header)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		return nil, &transportError{class: TransportWSDial, message: fmt.Sprintf("WebSocket dial %s failed: %v", endpoint, err)}
	}
	// Large answers (eth_getLogs over a range) arrive as one message.
	conn.SetReadLimit(-1)
	c := &wsConn{
		conn:         conn,
		timeout:      timeout,
		exactNumbers: cfg.StrictResponseComparison,
		nextID:       1,
		waiting:      map[int]chan wsAnswer{},
		done:         make(chan struct{}),
	}
	go c.read()
	return c, nil
}

func (c *wsConn) read() {
	defer close(c.done)
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			c.fail(&transportError{class: TransportWSClosed, message: fmt.Sprintf("WebSocket connection ended: %v", err)})
			return
		}
		msg, err := c.decode(data)
		if err != nil {
			c.fail(&transportError{class: TransportTruncated, message: fmt.Sprintf("failed to parse WebSocket message (%d bytes): %v", len(data), err)})
			return
		}
		id, isResponse := responseID(msg)
		c.mu.Lock()
		if !isResponse {
			c.notes++
			c.mu.Unlock()
			continue
		}
		ch, ok := c.waiting[id]
		delete(c.waiting, id)
		notes := c.notes
		if ok {
			c.notes = 0
		}
		c.mu.Unlock()
		if ok {
			ch <- wsAnswer{response: msg, notifications: notes}
		}
	}
}

// responseID is the id of a response. A message with a method, or without an
// integer id, is a notification.
func responseID(msg map[string]interface{}) (int, bool) {
	if _, ok := msg["method"]; ok {
		return 0, false
	}
	switch v := msg["id"].(type) {
	case float64:
		return int(v), v == float64(int(v))
	case json.Number:
		n, err := v.Int64()
		return int(n), err == nil
	}
	return 0, false
}

func (c *wsConn) decode(data []byte) (map[string]interface{}, error) {
	var msg map[string]interface{}
	if !c.exactNumbers {
		err := json.Unmarshal(data, &msg)
		return msg, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	err := dec.Decode(&msg)
	return msg, err
}

// fail ends every waiting call with err, and every later one.
func (c *wsConn) fail(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed == nil {
		c.closed = err
	}
	for id, ch := range c.waiting {
		ch <- wsAnswer{err: c.closed}
		delete(c.waiting, id)
	}
}

// call sends one request and waits for its answer, and returns how many
// notifications arrived before the answer since the previous one. It is never
// retried: a repeated eth_subscribe would make a second subscription.
func (c *wsConn) call(method string, params []interface{}) (map[string]interface{}, int, error) {
	ch := make(chan wsAnswer, 1)
	c.mu.Lock()
	if c.closed != nil {
		c.mu.Unlock()
		return nil, 0, c.closed
	}
	id := c.nextID
	c.nextID++
	c.waiting[id] = ch
	c.mu.Unlock()

	request, err := json.Marshal(JSONRPCRequest{JSONRPC: "2.0", Method: method, Params: params, ID: id})
	if err != nil {
		c.forget(id)
		return nil, 0, fmt.Errorf("failed to marshal request: %w", err)
	}
	c.writeMu.Lock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(c.timeout))
	err = c.conn.WriteMessage(websocket.TextMessage, request)
	c.writeMu.Unlock()
	if err != nil {
		c.forget(id)
		return nil, 0, &transportError{class: transportClassForError(err), message: fmt.Sprintf("WebSocket write failed: %v", err)}
	}

	timer := time.NewTimer(c.timeout)
	defer timer.Stop()
	select {
	case answer := <-ch:
		return answer.response, answer.notifications, answer.err
	case <-timer.C:
		c.forget(id)
		return nil, 0, &transportError{class: TransportTimeout, message: fmt.Sprintf("no WebSocket answer to %s within %s", method, c.timeout)}
	}
}

func (c *wsConn) forget(id int) {
	c.mu.Lock()
	delete(c.waiting, id)
	c.mu.Unlock()
}

func (c *wsConn) close() {
	c.writeMu.Lock()
	_ = c.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	c.writeMu.Unlock()
	_ = c.conn.Close()
	<-c.done
}

// openScenarioConns dials every client for a WebSocket scenario. A client that
// cannot be dialled maps to its dial error instead, and every step of the
// scenario is a transport failure for it.
func (c *Comparator) openScenarioConns() (map[string]*wsConn, map[string]error) {
	conns := make(map[string]*wsConn, len(c.config.Clients))
	failed := make(map[string]error)
	for _, client := range c.config.Clients {
		conn, err := dialWS(c.config, client)
		if err != nil {
			failed[client.Name] = err
			continue
		}
		conns[client.Name] = conn
	}
	return conns, failed
}

// noteNotifications records on a WebSocket step's result how many
// notifications each client received before answering it, and marks a
// difference for a client whose count is not the reference's. A step that is
// not compared — some client never answered — is left as it is.
func noteNotifications(result *ComparisonResult, reference string, counts map[string]int) {
	recorded := make(map[string]interface{}, len(counts))
	for name, n := range counts {
		recorded[name] = n
	}
	result.Metadata["notifications"] = recorded
	if len(result.TransportErrors) > 0 {
		return
	}
	want, ok := counts[reference]
	if !ok {
		return
	}
	for name, n := range counts {
		if name == reference || n == want {
			continue
		}
		diff, _ := result.Differences[name].(map[string]interface{})
		if diff == nil {
			diff = map[string]interface{}{}
		}
		diff["notification_differences"] = []DiffEntry{{
			Path:   "notifications",
			Type:   DiffTypeValueMismatch,
			Value1: want,
			Value2: n,
		}}
		result.Differences[name] = diff
	}
}

// validateTransport checks a scenario line's transport and waits.
func validateTransport(label, transport string, steps []ScenarioStep) error {
	switch transport {
	case "", ScenarioTransportWS:
	default:
		return fmt.Errorf("scenario %q: unknown transport %q (want %q, or none for HTTP)", label, transport, ScenarioTransportWS)
	}
	for i, step := range steps {
		if step.WaitMs < 0 || time.Duration(step.WaitMs)*time.Millisecond > maxScenarioWait {
			return fmt.Errorf("scenario %q step %d: wait_ms %d is outside 0..%d", label, i, step.WaitMs, maxScenarioWait.Milliseconds())
		}
	}
	return nil
}
