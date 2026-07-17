package comparator

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jsonrpc-bench/runner/types"
)

func writeRPCResult(w http.ResponseWriter, id int, result interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"jsonrpc": "2.0", "id": id, "result": result})
}

func decodeRPC(t *testing.T, r *http.Request) rpcRequest {
	t.Helper()
	body, _ := io.ReadAll(r.Body)
	var req rpcRequest
	_ = json.Unmarshal(body, &req)
	return req
}

func TestCompareIntegration_RetryRecovers(t *testing.T) {
	var mu sync.Mutex
	attempts := map[string]int{}
	flaky := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPC(t, r)
		if req.Method == "eth_chainId" {
			writeRPCResult(w, req.ID, "0x1")
			return
		}
		mu.Lock()
		attempts[req.Method]++
		n := attempts[req.Method]
		mu.Unlock()
		if n < 2 { // fail the first attempt with a retryable 5xx
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("busy"))
			return
		}
		writeRPCResult(w, req.ID, "0x123")
	}))
	t.Cleanup(flaky.Close)

	stable := newRPCFake(t, "0x1", func(req rpcRequest) interface{} { return "0x123" })

	cfg := &ComparisonConfig{
		Name:             "retry",
		Methods:          []string{"eth_blockNumber_variant1"},
		MethodRPCNames:   map[string]string{"eth_blockNumber_variant1": "eth_blockNumber"},
		CustomParameters: map[string][]interface{}{"eth_blockNumber_variant1": {}},
		Clients: []*types.ClientConfig{
			{Name: "flaky", URL: flaky.URL},
			{Name: "stable", URL: stable.URL},
		},
		TimeoutSeconds:   5,
		Concurrency:      1,
		MaxRetries:       3,
		RetryBaseDelayMs: 1,
		OutputDir:        t.TempDir(),
	}
	comp, err := NewComparator(cfg)
	if err != nil {
		t.Fatalf("NewComparator: %v", err)
	}
	if _, err := comp.Run(); err != nil {
		t.Fatalf("Run should recover after retry, got: %v", err)
	}
	res := comp.GetResults()[0]
	if len(res.TransportErrors) != 0 {
		t.Errorf("retry should have cleared transport errors, got %v", res.TransportErrors)
	}
	if len(res.Differences) != 0 {
		t.Errorf("both clients returned 0x123 after retry, got diffs %v", res.Differences)
	}
}

func TestCompareIntegration_NonFatalTransport(t *testing.T) {
	good := newRPCFake(t, "0x1", func(req rpcRequest) interface{} {
		if req.Method == "eth_blockNumber" {
			return "0x5"
		}
		return "0xbal"
	})
	// partial answers chainId and eth_blockNumber but never eth_getBalance.
	partial := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPC(t, r)
		switch req.Method {
		case "eth_chainId":
			writeRPCResult(w, req.ID, "0x1")
		case "eth_blockNumber":
			writeRPCResult(w, req.ID, "0x5")
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("down"))
		}
	}))
	t.Cleanup(partial.Close)

	cfg := &ComparisonConfig{
		Name: "nonfatal",
		Methods: []string{
			"eth_blockNumber_variant1",
			"eth_getBalance_variant1",
		},
		MethodRPCNames: map[string]string{
			"eth_blockNumber_variant1": "eth_blockNumber",
			"eth_getBalance_variant1":  "eth_getBalance",
		},
		CustomParameters: map[string][]interface{}{
			"eth_blockNumber_variant1": {},
			"eth_getBalance_variant1":  {"0xabc", "0x1"},
		},
		Clients: []*types.ClientConfig{
			{Name: "good", URL: good.URL},
			{Name: "partial", URL: partial.URL},
		},
		TimeoutSeconds:   5,
		Concurrency:      1,
		MaxRetries:       2,
		RetryBaseDelayMs: 1,
		OutputDir:        t.TempDir(),
	}
	comp, err := NewComparator(cfg)
	if err != nil {
		t.Fatalf("NewComparator: %v", err)
	}
	if _, err := comp.Run(); err != nil {
		t.Fatalf("a single dead call must not abort the run, got: %v", err)
	}

	byMethod := map[string]ComparisonResult{}
	for _, r := range comp.GetResults() {
		byMethod[r.Method] = r
	}
	if len(byMethod) != 2 {
		t.Fatalf("expected both calls recorded, got %v", byMethod)
	}
	if got := byMethod["eth_getBalance_variant1"]; len(got.TransportErrors) == 0 {
		t.Errorf("expected transport error recorded for the dead call, got %+v", got)
	}
	if got := byMethod["eth_blockNumber_variant1"]; len(got.Differences) != 0 || len(got.TransportErrors) != 0 {
		t.Errorf("blockNumber should be clean, got %+v", got)
	}
	if s := comp.Summarize(); s.TransportError != 1 {
		t.Errorf("summary transport-error = %d, want 1", s.TransportError)
	}
}

func TestCompareIntegration_DiffOnly(t *testing.T) {
	a := newRPCFake(t, "0x1", func(req rpcRequest) interface{} {
		if req.Method == "eth_blockNumber" {
			return "0x123"
		}
		return "0xaaa"
	})
	b := newRPCFake(t, "0x1", func(req rpcRequest) interface{} {
		if req.Method == "eth_blockNumber" {
			return "0x123"
		}
		return "0xbbb" // mismatch on eth_getBalance
	})

	dir := t.TempDir()
	cfg := &ComparisonConfig{
		Name: "diffonly",
		Methods: []string{
			"eth_blockNumber_variant1",
			"eth_getBalance_variant1",
		},
		MethodRPCNames: map[string]string{
			"eth_blockNumber_variant1": "eth_blockNumber",
			"eth_getBalance_variant1":  "eth_getBalance",
		},
		CustomParameters: map[string][]interface{}{
			"eth_blockNumber_variant1": {},
			"eth_getBalance_variant1":  {"0xabc", "0x1"},
		},
		Clients: []*types.ClientConfig{
			{Name: "a", URL: a.URL},
			{Name: "b", URL: b.URL},
		},
		TimeoutSeconds: 5,
		Concurrency:    1,
		DiffOnly:       true,
		OutputDir:      dir,
	}
	comp, err := NewComparator(cfg)
	if err != nil {
		t.Fatalf("NewComparator: %v", err)
	}
	if _, err := comp.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !comp.HasDifferences() {
		t.Error("expected HasDifferences true (fail-on-diff would trip)")
	}

	jsonPath := filepath.Join(dir, "results.json")
	if err := comp.SaveResults(jsonPath); err != nil {
		t.Fatalf("SaveResults: %v", err)
	}
	data, _ := os.ReadFile(jsonPath)
	var results []ComparisonResult
	if err := json.Unmarshal(data, &results); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(results) != 1 || results[0].Method != "eth_getBalance_variant1" {
		t.Fatalf("--diff-only should keep only the differing call, got %d (%v)", len(results), results)
	}
}
