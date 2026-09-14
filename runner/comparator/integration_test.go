package comparator

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
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
	var doc ComparisonResultsDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal results: %v", err)
	}
	if doc.SchemaVersion != ComparisonResultsSchemaVersion {
		t.Errorf("schema_version = %d, want %d", doc.SchemaVersion, ComparisonResultsSchemaVersion)
	}
	if doc.Summary.Total != 3 {
		t.Errorf("summary.total = %d, want 3", doc.Summary.Total)
	}
	if len(doc.ClientRefs) != 2 {
		t.Errorf("client_refs = %v, want 2 entries", doc.ClientRefs)
	}
	results := doc.Results
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

// recordingFake answers with a raw JSON result body and remembers every
// request it received, so a test can assert what was actually put on the wire
// rather than what the caller intended to send.
type recordingFake struct {
	srv   *httptest.Server
	mu    sync.Mutex
	calls []rpcRequest
}

func newRecordingFake(t *testing.T, chainID string, result func(req rpcRequest) string) *recordingFake {
	t.Helper()
	f := &recordingFake{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		raw := `"` + chainID + `"`
		if req.Method != "eth_chainId" {
			f.mu.Lock()
			f.calls = append(f.calls, req)
			f.mu.Unlock()
			raw = result(req)
		}
		w.Header().Set("Content-Type", "application/json")
		// Written as raw bytes: a large integer routed through
		// map[string]interface{} would be re-encoded from float64 and lose the
		// precision this test is about.
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":%s}`, req.ID, raw)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *recordingFake) received() []rpcRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]rpcRequest(nil), f.calls...)
}

// wireTransformDoc is the provenance shape this test reads back.
type wireTransformDoc struct {
	StrictResponseComparison bool `json:"strict_response_comparison"`
	WireTransformations      []struct {
		Method          string        `json:"method"`
		RPCMethod       string        `json:"rpc_method"`
		OriginalParams  []interface{} `json:"original_params"`
		EffectiveParams []interface{} `json:"effective_params"`
	} `json:"wire_transformations"`
}

func runStrictPair(t *testing.T, strict bool, methods map[string][]interface{}, rpcNames map[string]string, blockOverride string, result func(req rpcRequest) string) (*Comparator, *recordingFake, *recordingFake, string) {
	t.Helper()
	a := newRecordingFake(t, "0x1", result)
	b := newRecordingFake(t, "0x1", result)

	names := make([]string, 0, len(methods))
	for id := range methods {
		names = append(names, id)
	}
	sort.Strings(names)

	dir := t.TempDir()
	cfg := &ComparisonConfig{
		Name:                     "strict-integration",
		Methods:                  names,
		MethodRPCNames:           rpcNames,
		CustomParameters:         methods,
		BlockOverride:            blockOverride,
		StrictResponseComparison: strict,
		Clients: []*types.ClientConfig{
			{Name: "baseline", URL: a.srv.URL},
			{Name: "candidate", URL: b.srv.URL},
		},
		TimeoutSeconds: 5,
		Concurrency:    1,
		OutputDir:      dir,
	}
	comp, err := NewComparator(cfg)
	if err != nil {
		t.Fatalf("NewComparator: %v", err)
	}
	if _, err := comp.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return comp, a, b, dir
}

// TestStrictIntegration_WireParamsInProvenance is the wire-capture check: the
// effective params recorded in comparison-provenance.json must be exactly what
// the transport sent, and the originals must survive beside them.
func TestStrictIntegration_WireParamsInProvenance(t *testing.T) {
	const pin = "0x77"
	hash := "0x" + strings.Repeat("aa", 32)
	methods := map[string][]interface{}{
		"eth_call_variant1":    {map[string]interface{}{"to": "0x1"}, "latest"},
		"eth_getLogs_variant1": {map[string]interface{}{"blockHash": hash}},
	}
	rpcNames := map[string]string{
		"eth_call_variant1":    "eth_call",
		"eth_getLogs_variant1": "eth_getLogs",
	}
	comp, a, _, dir := runStrictPair(t, true, methods, rpcNames, pin, func(req rpcRequest) string {
		return `"0xab"`
	})

	provPath := filepath.Join(dir, "comparison-provenance.json")
	if err := comp.SaveProvenance(provPath); err != nil {
		t.Fatalf("SaveProvenance: %v", err)
	}
	data, err := os.ReadFile(provPath)
	if err != nil {
		t.Fatalf("read provenance: %v", err)
	}
	var doc wireTransformDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal provenance: %v", err)
	}
	if !doc.StrictResponseComparison {
		t.Error("provenance must record that the run was strict")
	}

	// eth_getLogs addressed a block by hash, so strict mode left it alone and
	// there is nothing to record for it.
	if len(doc.WireTransformations) != 1 {
		t.Fatalf("wire_transformations = %v, want exactly the eth_call rewrite", doc.WireTransformations)
	}
	tr := doc.WireTransformations[0]
	if tr.Method != "eth_call_variant1" || tr.RPCMethod != "eth_call" {
		t.Errorf("recorded %s/%s, want eth_call_variant1/eth_call", tr.Method, tr.RPCMethod)
	}
	if got := tr.OriginalParams[1]; got != "latest" {
		t.Errorf("original params must keep the tag, got %v", got)
	}
	if got := tr.EffectiveParams[1]; got != pin {
		t.Errorf("effective params must carry the pin, got %v", got)
	}

	// The effective params are what the endpoint received, param for param.
	wire := make(map[string][]interface{})
	for _, req := range a.received() {
		wire[req.Method] = req.Params
	}
	if !reflect.DeepEqual(wire["eth_call"], tr.EffectiveParams) {
		t.Errorf("provenance effective params %v != wire %v", tr.EffectiveParams, wire["eth_call"])
	}
	if !reflect.DeepEqual(wire["eth_getLogs"], []interface{}{map[string]interface{}{"blockHash": hash}}) {
		t.Errorf("a blockHash filter must reach the wire unchanged under strict, got %v", wire["eth_getLogs"])
	}
}

// TestStrictIntegration_BlockHashFilterDefault is the same run without strict:
// the range is still injected, and the transformation is recorded, so the
// default behaviour stays visible rather than silent.
func TestStrictIntegration_BlockHashFilterDefault(t *testing.T) {
	const pin = "0x77"
	hash := "0x" + strings.Repeat("aa", 32)
	methods := map[string][]interface{}{
		"eth_getLogs_variant1": {map[string]interface{}{"blockHash": hash}},
	}
	rpcNames := map[string]string{"eth_getLogs_variant1": "eth_getLogs"}
	comp, a, _, _ := runStrictPair(t, false, methods, rpcNames, pin, func(req rpcRequest) string {
		return `[]`
	})

	sent := a.received()
	if len(sent) != 1 {
		t.Fatalf("received %v, want one eth_getLogs", sent)
	}
	filter := sent[0].Params[0].(map[string]interface{})
	if filter["fromBlock"] != pin || filter["toBlock"] != pin {
		t.Errorf("the default must still inject the range, got %v", filter)
	}
	prov := comp.Provenance()
	if strict := prov["strict_response_comparison"]; strict != false {
		t.Errorf("strict_response_comparison = %v, want false", strict)
	}
	if got := prov["wire_transformations"].([]wireTransform); len(got) != 1 {
		t.Errorf("the injection must be recorded, got %v", got)
	}
}

// TestStrictIntegration_LargeIntegersDiffer runs the float64 collision through
// the real transport: two integers above 2**53 that differ by one.
func TestStrictIntegration_LargeIntegersDiffer(t *testing.T) {
	methods := map[string][]interface{}{"eth_call_variant1": {map[string]interface{}{"to": "0x1"}}}
	rpcNames := map[string]string{"eth_call_variant1": "eth_call"}

	for _, tc := range []struct {
		name      string
		strict    bool
		wantDiffs int
	}{
		{"default hides it", false, 0},
		{"strict catches it", true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first := true
			var mu sync.Mutex
			comp, _, _, _ := runStrictPair(t, tc.strict, methods, rpcNames, "", func(req rpcRequest) string {
				mu.Lock()
				defer mu.Unlock()
				if first {
					first = false
					return "12345678901234567890"
				}
				return "12345678901234567891"
			})
			results := comp.GetResults()
			if len(results) != 1 {
				t.Fatalf("results = %v, want 1", results)
			}
			if got := len(results[0].Differences); got != tc.wantDiffs {
				t.Errorf("differences = %d (%v), want %d", got, results[0].Differences, tc.wantDiffs)
			}
		})
	}
}
