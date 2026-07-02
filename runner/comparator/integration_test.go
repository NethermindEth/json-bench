package comparator

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jsonrpc-bench/runner/types"
)

// rpcRequest is the minimal JSON-RPC 2.0 request shape the comparator sends.
type rpcRequest struct {
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
	ID     int           `json:"id"`
}

// newRPCFake spins up an httptest.Server that answers eth_chainId with the
// given chain id and dispatches other methods to the supplied handler.
func newRPCFake(t *testing.T, chainID string, handler func(req rpcRequest) interface{}) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		var req rpcRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		var result interface{}
		if req.Method == "eth_chainId" {
			result = chainID
		} else {
			result = handler(req)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  result,
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCompareIntegration_MatchAndMismatch(t *testing.T) {
	// Client A returns fixed values. Client B agrees on eth_blockNumber but
	// disagrees on eth_getBalance for the "vitalik-latest" call.
	geth := newRPCFake(t, "0x1", func(req rpcRequest) interface{} {
		switch req.Method {
		case "eth_blockNumber":
			return "0x123"
		case "eth_getBalance":
			if len(req.Params) > 1 && req.Params[1] == "latest" {
				return "0xabc"
			}
			return "0xdef"
		}
		return nil
	})
	nethermind := newRPCFake(t, "0x1", func(req rpcRequest) interface{} {
		switch req.Method {
		case "eth_blockNumber":
			return "0x123"
		case "eth_getBalance":
			if len(req.Params) > 1 && req.Params[1] == "latest" {
				// deliberate mismatch on the latest-balance variant
				return "0x999"
			}
			return "0xdef"
		}
		return nil
	})

	dir := t.TempDir()
	cfg := &ComparisonConfig{
		Name:        "integration",
		Description: "two-client fake",
		Methods: []string{
			"eth_blockNumber_variant1",
			"eth_getBalance_vitalik-latest",
			"eth_getBalance_variant2",
		},
		MethodRPCNames: map[string]string{
			"eth_blockNumber_variant1":      "eth_blockNumber",
			"eth_getBalance_vitalik-latest": "eth_getBalance",
			"eth_getBalance_variant2":       "eth_getBalance",
		},
		CustomParameters: map[string][]interface{}{
			"eth_blockNumber_variant1":      {},
			"eth_getBalance_vitalik-latest": {"0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045", "latest"},
			"eth_getBalance_variant2":       {"0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045", "0x1000"},
		},
		Clients: []*types.ClientConfig{
			{Name: "geth", URL: geth.URL},
			{Name: "nethermind", URL: nethermind.URL},
		},
		TimeoutSeconds: 5,
		Concurrency:    2,
		OutputDir:      dir,
	}

	comp, err := NewComparator(cfg)
	if err != nil {
		t.Fatalf("NewComparator: %v", err)
	}
	if _, err := comp.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	jsonPath := filepath.Join(dir, "results.json")
	if err := comp.SaveResults(jsonPath); err != nil {
		t.Fatalf("SaveResults: %v", err)
	}
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read results: %v", err)
	}
	var results []ComparisonResult
	if err := json.Unmarshal(data, &results); err != nil {
		t.Fatalf("unmarshal results: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results len = %d, want 3 (%v)", len(results), results)
	}

	byMethod := make(map[string]ComparisonResult, len(results))
	for _, r := range results {
		byMethod[r.Method] = r
	}

	// eth_blockNumber and the 0x1000-block balance call should match; the
	// vitalik-latest variant should surface a diff.
	if got := byMethod["eth_blockNumber_variant1"]; len(got.Differences) != 0 {
		t.Errorf("eth_blockNumber should match, got diffs: %v", got.Differences)
	}
	if got := byMethod["eth_getBalance_variant2"]; len(got.Differences) != 0 {
		t.Errorf("eth_getBalance_variant2 should match, got diffs: %v", got.Differences)
	}
	mismatch, ok := byMethod["eth_getBalance_vitalik-latest"]
	if !ok {
		t.Fatalf("missing result for eth_getBalance_vitalik-latest: %v", byMethod)
	}
	if len(mismatch.Differences) == 0 {
		t.Errorf("expected diff for vitalik-latest, got none: %+v", mismatch)
	}
	// The wire-level method must have been eth_getBalance, not the identifier
	// — this guards the MethodRPCNames-based extraction against a regression
	// where the identifier is sent as the RPC method.
	gethResp, ok := mismatch.Responses["geth"].(map[string]interface{})
	if !ok {
		t.Fatalf("geth response should be a map, got %T", mismatch.Responses["geth"])
	}
	if gethResp["result"] != "0xabc" {
		t.Errorf("wire-level eth_getBalance result should be 0xabc, got %v", gethResp["result"])
	}
}
