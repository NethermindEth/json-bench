package stubnode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"

	"github.com/gorilla/websocket"
)

// Answer produces the JSON-RPC response for one payload with no HTTP framing
// around it, which is what a WebSocket or IPC client sees.
//
// The HTTP-shaped faults are deliberately absent here rather than translated: a
// socket transport has no status code, so a node cannot answer 500 over one. The
// failures it can express — latency, a JSON-RPC error, a null result, never
// answering, and hanging up — all still apply.
//
// A nil response means the request is not being answered. keepOpen reports
// whether the connection survives it.
func (s *Stub) Answer(ctx context.Context, body []byte) (response []byte, keepOpen bool) {
	if firstNonSpace(body) == '[' {
		return s.answerBatch(ctx, body)
	}

	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil || req.Method == "" {
		s.record("", OutcomeBadInput)
		return mustJSON(map[string]any{
			"jsonrpc": "2.0", "id": nil,
			"error": map[string]any{"code": -32700, "message": "stubnode: parse error"},
		}), true
	}

	if result, ok := s.identity(req.Method); ok {
		s.record(req.Method, OutcomeOK)
		return mustJSON(map[string]any{
			"jsonrpc": "2.0", "id": rawOrNull(req.ID), "result": result,
		}), true
	}

	key := requestKey(req.ID, body)
	method := s.methodConfig(req.Method)

	if !s.acquireWorker(ctx) {
		s.record(req.Method, OutcomeTimeout)
		return nil, true
	}
	defer s.releaseWorker()

	if !s.sleep(ctx, method.Latency, req.Method, key) {
		s.record(req.Method, OutcomeTimeout)
		return nil, true
	}

	switch outcome := s.fault(req.Method, key); outcome {
	case OutcomeTruncated:
		// Hanging up mid-answer is the socket's version of a body cut short.
		s.record(req.Method, OutcomeTruncated)
		return nil, false
	case OutcomeTimeout:
		s.hang(ctx, req.Method)
		return nil, true
	}

	return mustJSON(s.batchMember(req, method, key)), true
}

func (s *Stub) answerBatch(ctx context.Context, body []byte) ([]byte, bool) {
	var raw []json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		s.record("", OutcomeBadInput)
		return mustJSON(map[string]any{
			"jsonrpc": "2.0", "id": nil,
			"error": map[string]any{"code": -32700, "message": "stubnode: parse error"},
		}), true
	}

	if limit := s.cfg.Node.MaxBatchSize; limit > 0 && len(raw) > limit {
		s.record("", OutcomeRPCError)
		return mustJSON(map[string]any{
			"jsonrpc": "2.0", "id": nil,
			"error": map[string]any{"code": -32600, "message": "stubnode: batch too large"},
		}), true
	}

	responses := make([]map[string]any, 0, len(raw))
	var slowest int64
	for _, member := range raw {
		var req rpcRequest
		if err := json.Unmarshal(member, &req); err != nil || req.Method == "" {
			s.record("", OutcomeBadInput)
			responses = append(responses, map[string]any{
				"jsonrpc": "2.0", "id": nil,
				"error": map[string]any{"code": -32700, "message": "stubnode: parse error"},
			})
			continue
		}
		key := requestKey(req.ID, member)
		method := s.methodConfig(req.Method)
		if d := int64(s.latency(method.Latency, req.Method, key)); d > slowest {
			slowest = d
		}
		responses = append(responses, s.batchMember(req, method, key))
	}

	if !s.sleepFor(ctx, slowest) {
		return nil, true
	}

	// Answered in reverse on purpose: the spec promises no ordering, so a client
	// matching by position fails here rather than in production.
	for i, j := 0, len(responses)-1; i < j; i, j = i+1, j-1 {
		responses[i], responses[j] = responses[j], responses[i]
	}
	return mustJSON(responses), true
}

// ServeWebSocket upgrades an HTTP request and answers JSON-RPC over the socket
// until the client goes away. Requests are answered concurrently, because a node
// multiplexing one connection does not serialise them.
func (s *Stub) ServeWebSocket(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}
	socket, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer socket.Close()

	ctx := r.Context()
	var writeMu sync.Mutex
	var inFlight sync.WaitGroup
	defer inFlight.Wait()

	for {
		_, frame, err := socket.ReadMessage()
		if err != nil {
			return
		}
		inFlight.Add(1)
		go func(payload []byte) {
			defer inFlight.Done()
			response, keepOpen := s.Answer(ctx, payload)
			if response != nil {
				writeMu.Lock()
				_ = socket.WriteMessage(websocket.TextMessage, response)
				writeMu.Unlock()
			}
			if !keepOpen {
				socket.Close()
			}
		}(frame)
	}
}

// ListenIPC serves JSON-RPC over a Unix domain socket at path, which is how a
// node is reached from the machine it runs on.
func (s *Stub) ListenIPC(path string) (net.Listener, error) {
	// A socket left behind by a previous process makes bind fail with a message
	// that reads like a permissions problem.
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s (a Unix socket path is limited to about 104 bytes): %w", path, err)
	}
	go s.acceptIPC(listener)
	return listener, nil
}

func (s *Stub) acceptIPC(listener net.Listener) {
	for {
		socket, err := listener.Accept()
		if err != nil {
			return
		}
		go s.serveIPCConn(socket)
	}
}

func (s *Stub) serveIPCConn(socket net.Conn) {
	defer socket.Close()

	ctx := context.Background()
	dec := json.NewDecoder(socket)
	var writeMu sync.Mutex
	var inFlight sync.WaitGroup
	defer inFlight.Wait()

	for {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			if err != io.EOF {
				return
			}
			return
		}
		inFlight.Add(1)
		go func(payload []byte) {
			defer inFlight.Done()
			response, keepOpen := s.Answer(ctx, payload)
			if response != nil {
				writeMu.Lock()
				_, _ = socket.Write(response)
				writeMu.Unlock()
			}
			if !keepOpen {
				socket.Close()
			}
		}(raw)
	}
}

func mustJSON(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"stubnode: encode failed"}}`)
	}
	return raw
}
