package comparator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/jsonrpc-bench/runner/types"
)

// #2 — --diff-only drops identical calls and truncates large bodies while
// preserving diff entries and leaving the stored results unmutated.
func TestResultsForOutputDiffOnlyTruncates(t *testing.T) {
	big := strings.Repeat("a", 10000)
	c := &Comparator{
		config: &ComparisonConfig{DiffOnly: true, MaxResponseBytes: 100},
		results: []ComparisonResult{
			{Method: "same"},
			{
				Method:      "diff",
				Differences: map[string]interface{}{"nodeB": "changed"},
				Responses:   map[string]interface{}{"nodeA": big, "nodeB": "small"},
			},
		},
	}

	out := c.resultsForOutput()
	if len(out) != 1 || out[0].Method != "diff" {
		t.Fatalf("diff-only should keep only the differing call, got %v", out)
	}
	nodeA, ok := out[0].Responses["nodeA"].(map[string]interface{})
	if !ok || nodeA["_truncated"] != true {
		t.Errorf("large body should be truncated, got %v", out[0].Responses["nodeA"])
	}
	if out[0].Responses["nodeB"] != "small" {
		t.Errorf("small body should be kept, got %v", out[0].Responses["nodeB"])
	}
	if len(out[0].Differences) == 0 {
		t.Error("diff entries must be preserved")
	}
	if _, ok := c.results[1].Responses["nodeA"].(string); !ok {
		t.Error("stored results must not be mutated by resultsForOutput")
	}
}

// #2 — the escape hatch: keeping bodies means no truncation is applied.
func TestResultsForOutputDiffOnlyKeepBodies(t *testing.T) {
	big := strings.Repeat("a", 10000)
	c := &Comparator{
		config: &ComparisonConfig{DiffOnly: true}, // no MaxResponseBytes => keep
		results: []ComparisonResult{
			{Method: "diff", Differences: map[string]interface{}{"n": 1}, Responses: map[string]interface{}{"nodeA": big}},
		},
	}
	out := c.resultsForOutput()
	if out[0].Responses["nodeA"] != big {
		t.Error("without a cap, diff-only should keep full bodies")
	}
}

// #3 — env-classified mismatches count separately and do not trip real-diff
// gating.
func TestSummarizeRealVsEnv(t *testing.T) {
	c := &Comparator{
		config: &ComparisonConfig{},
		results: []ComparisonResult{
			{Method: "real", Differences: map[string]interface{}{"nodeB": "x"}},
			{Method: "env", Differences: map[string]interface{}{"nodeB": "x"}, ErrorClass: map[string]string{"nodeB": "no_state"}},
			{Method: "same"},
			{Method: "dead", TransportErrors: map[string]string{"nodeB": "boom"}},
		},
	}

	s := c.Summarize()
	if s.Differ != 1 {
		t.Errorf("Differ (real) = %d, want 1", s.Differ)
	}
	if s.DifferEnv != 1 {
		t.Errorf("DifferEnv = %d, want 1", s.DifferEnv)
	}
	if s.Identical != 1 {
		t.Errorf("Identical = %d, want 1", s.Identical)
	}
	if s.TransportError != 1 {
		t.Errorf("TransportError = %d, want 1", s.TransportError)
	}
	if s.EnvError["no_state"] != 1 {
		t.Errorf("EnvError[no_state] = %d, want 1", s.EnvError["no_state"])
	}

	if !c.HasRealDifferences() {
		t.Error("HasRealDifferences should be true")
	}
	if !c.HasEnvDifferences() {
		t.Error("HasEnvDifferences should be true")
	}
}

// #3 — an env-only run has no real differences, so default --fail-on-diff
// (which gates on HasRealDifferences) must not trip.
func TestEnvOnlyHasNoRealDifference(t *testing.T) {
	c := &Comparator{
		config: &ComparisonConfig{},
		results: []ComparisonResult{
			{Method: "env", Differences: map[string]interface{}{"nodeB": "x"}, ErrorClass: map[string]string{"nodeB": "range_cap"}},
		},
	}
	if c.HasRealDifferences() {
		t.Error("env-only run must report no real differences")
	}
	if !c.HasEnvDifferences() {
		t.Error("env-only run must report env differences")
	}
	if s := c.Summarize(); s.Differ != 0 || s.DifferEnv != 1 {
		t.Errorf("summary = %+v, want Differ=0 DifferEnv=1", s)
	}
}

// A call that lost one client of three used to be counted as a difference:
// Summarize tested hasDifferences() before the transport arm. It was never a
// complete comparison, so it belongs in the transport bucket alone.
func TestSummarizeIncompleteCallIsNotADifference(t *testing.T) {
	c := &Comparator{
		config: &ComparisonConfig{},
		results: []ComparisonResult{{
			Method:              "eth_getLogs_variant1",
			Differences:         map[string]interface{}{"nodeC": "x"},
			TransportErrors:     map[string]string{"nodeA": "HTTP request failed with status 429: {}"},
			TransportErrorClass: map[string]string{"nodeA": TransportRateLimited},
		}},
	}

	s := c.Summarize()
	if s.TransportError != 1 {
		t.Errorf("TransportError = %d, want 1", s.TransportError)
	}
	if s.RateLimited != 1 {
		t.Errorf("RateLimited = %d, want 1", s.RateLimited)
	}
	if s.Differ != 0 || s.DifferEnv != 0 || s.Identical != 0 {
		t.Errorf("summary = %+v, want the call bucketed only as transport-error", s)
	}
	if s.Identical+s.Differ+s.DifferEnv+s.TransportError+s.SchemaError != s.Total {
		t.Errorf("summary arms must sum to Total: %+v", s)
	}
	if !c.HasTransportErrors() {
		t.Error("HasTransportErrors should be true")
	}
}

// The truncation placeholder must not read like a response: every key is
// underscore-prefixed, and an error keeps the part worth reading.
func TestTruncationMarker(t *testing.T) {
	big := map[string]interface{}{"jsonrpc": "2.0", "result": map[string]interface{}{"blob": "0x" + strings.Repeat("ab", 200)}}
	errResp := map[string]interface{}{"jsonrpc": "2.0", "error": map[string]interface{}{
		"code": float64(-32000), "message": "missing trie node 0x" + strings.Repeat("cd", 200),
	}}

	out := truncateResponses(map[string]interface{}{"big": big, "err": errResp}, 64)

	marker, ok := out["big"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected a marker for the oversized result, got %T", out["big"])
	}
	if marker["_truncated"] != true || marker["_kind"] != "result" {
		t.Errorf("result marker = %v", marker)
	}
	if _, ok := marker["_bytes"].(int); !ok {
		t.Errorf("marker should record the original size, got %v", marker["_bytes"])
	}
	for key := range marker {
		if key[0] != '_' {
			t.Errorf("marker key %q is not underscore-prefixed; tooling could read it as a response", key)
		}
	}

	errMarker, ok := out["err"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected a marker for the oversized error, got %T", out["err"])
	}
	if errMarker["_kind"] != "error" {
		t.Errorf("error marker kind = %v, want \"error\"", errMarker["_kind"])
	}
	summary, ok := errMarker["_error"].(map[string]interface{})
	if !ok || summary["code"] != float64(-32000) {
		t.Errorf("error marker should keep the code/message summary, got %v", errMarker["_error"])
	}
}

// Every entry of the results document names its request by identity and by the
// wire-level method, so a consumer reconciling a run does not have to strip a
// loader-invented _variantN suffix or match on array position.
func TestResultsCarryRequestIdentity(t *testing.T) {
	node := newRPCFake(t, "0x1", func(req rpcRequest) interface{} { return "0x1" })

	dir := t.TempDir()
	cfg := &ComparisonConfig{
		Name:    "identity",
		Methods: []string{"eth_getBalance_variant1", "eth_getBalance_variant2", "eth_chainId_variant1"},
		MethodRPCNames: map[string]string{
			"eth_getBalance_variant1": "eth_getBalance",
			"eth_getBalance_variant2": "eth_getBalance",
			"eth_chainId_variant1":    "eth_chainId",
		},
		CustomParameters: map[string][]interface{}{
			"eth_getBalance_variant1": {"0xabc", "0x10"},
			"eth_getBalance_variant2": {"0xabc", "0x11"},
			"eth_chainId_variant1":    {},
		},
		Clients: []*types.ClientConfig{
			{Name: "baseline", URL: node.URL},
			{Name: "candidate", URL: node.URL},
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
	path := filepath.Join(dir, "comparison-results.json")
	if err := comp.SaveResults(path); err != nil {
		t.Fatalf("SaveResults: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read results: %v", err)
	}
	var doc ComparisonResultsDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal results: %v", err)
	}
	if len(doc.Results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(doc.Results))
	}

	ids := map[string]string{}
	for _, result := range doc.Results {
		if result.RequestID == "" {
			t.Errorf("%s has no request_id", result.Method)
			continue
		}
		want, err := RequestID(result.RPCMethod, cfg.CustomParameters[result.Method])
		if err != nil {
			t.Fatalf("RequestID: %v", err)
		}
		if result.RequestID != want {
			t.Errorf("%s request_id = %s, want %s", result.Method, result.RequestID, want)
		}
		ids[result.Method] = result.RequestID
	}

	// The identity is of the wire method, not of the variant identifier: two
	// variants of the same method differ only by their params.
	if ids["eth_getBalance_variant1"] == ids["eth_getBalance_variant2"] {
		t.Error("two variants with different params must have different identities")
	}
	if doc.Results[0].RPCMethod == "" {
		t.Error("rpc_method must be recorded alongside the identifier")
	}
	for _, result := range doc.Results {
		if strings.HasSuffix(result.RPCMethod, "_variant1") {
			t.Errorf("rpc_method = %q, want the wire method", result.RPCMethod)
		}
	}
	if want, _ := RequestID("eth_chainId", nil); ids["eth_chainId_variant1"] != want {
		t.Errorf("a no-params call's identity = %s, want %s", ids["eth_chainId_variant1"], want)
	}
}

// The identity is of the request as the corpus recorded it, not of what the
// block override put on the wire. It has to be: the manifest a consumer
// reconciles against was written before any override existed.
func TestResultIdentityIsOfTheOriginalRequest(t *testing.T) {
	var mu sync.Mutex
	var onTheWire [][]interface{}
	node := newRPCFake(t, "0x1", func(req rpcRequest) interface{} {
		mu.Lock()
		onTheWire = append(onTheWire, req.Params)
		mu.Unlock()
		return "0x1"
	})

	dir := t.TempDir()
	cfg := &ComparisonConfig{
		Name:             "override-identity",
		Methods:          []string{"eth_getBalance_variant1"},
		MethodRPCNames:   map[string]string{"eth_getBalance_variant1": "eth_getBalance"},
		CustomParameters: map[string][]interface{}{"eth_getBalance_variant1": {"0xabc", "latest"}},
		BlockOverride:    "0x1406f40",
		Clients: []*types.ClientConfig{
			{Name: "baseline", URL: node.URL},
			{Name: "candidate", URL: node.URL},
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
	results := comp.GetResults()
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	original, err := RequestID("eth_getBalance", []interface{}{"0xabc", "latest"})
	if err != nil {
		t.Fatalf("RequestID: %v", err)
	}
	effective, err := RequestID("eth_getBalance", []interface{}{"0xabc", "0x1406f40"})
	if err != nil {
		t.Fatalf("RequestID: %v", err)
	}
	if results[0].RequestID != original {
		t.Errorf("request_id = %s, want the original %s", results[0].RequestID, original)
	}
	if results[0].RequestID == effective {
		t.Fatal("the fixture is wrong: the two forms must differ")
	}

	// And the override did happen: the node was asked for the pinned block, not
	// for "latest". The identity is of the request, the wire carries the call.
	mu.Lock()
	sent := false
	for _, params := range onTheWire {
		if len(params) == 2 && params[1] == "0x1406f40" {
			sent = true
		}
	}
	mu.Unlock()
	if !sent {
		t.Errorf("expected the override on the wire, saw %v", onTheWire)
	}

	// The effective form is not lost either: request_id names the request as
	// recorded, and wire_transformations gives the params it was sent as. The
	// two together are what makes a run reproducible — neither alone is.
	transforms, ok := comp.Provenance()["wire_transformations"].([]wireTransform)
	if !ok || len(transforms) != 1 {
		t.Fatalf("expected one recorded transform, got %v", comp.Provenance()["wire_transformations"])
	}
	if got := transforms[0].OriginalParams[1]; got != "latest" {
		t.Errorf("original params[1] = %v, want latest", got)
	}
	if got := transforms[0].EffectiveParams[1]; got != "0x1406f40" {
		t.Errorf("effective params[1] = %v, want the pinned block", got)
	}
	originalOfTransform, err := RequestID(transforms[0].RPCMethod, transforms[0].OriginalParams)
	if err != nil {
		t.Fatalf("RequestID: %v", err)
	}
	if originalOfTransform != results[0].RequestID {
		t.Errorf("the transform's original does not identify the result: %s vs %s",
			originalOfTransform, results[0].RequestID)
	}
}

// A call dropped by --skip-above-head is named by identity in the provenance,
// which is the only place it appears at all: it produces no result.
func TestSkippedCallsCarryRequestIdentity(t *testing.T) {
	node := newRPCFake(t, "0x1", func(req rpcRequest) interface{} {
		if req.Method == "eth_blockNumber" {
			return "0x10"
		}
		return "0x1"
	})

	dir := t.TempDir()
	cfg := &ComparisonConfig{
		Name:    "skips",
		Methods: []string{"eth_getBalance_low", "eth_getBalance_high"},
		MethodRPCNames: map[string]string{
			"eth_getBalance_low":  "eth_getBalance",
			"eth_getBalance_high": "eth_getBalance",
		},
		CustomParameters: map[string][]interface{}{
			"eth_getBalance_low":  {"0xabc", "0x1"},
			"eth_getBalance_high": {"0xabc", "0xffff"},
		},
		SkipAboveHead: true,
		Clients: []*types.ClientConfig{
			{Name: "baseline", URL: node.URL},
			{Name: "candidate", URL: node.URL},
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

	provPath := filepath.Join(dir, "comparison-provenance.json")
	if err := comp.SaveProvenance(provPath); err != nil {
		t.Fatalf("SaveProvenance: %v", err)
	}
	data, err := os.ReadFile(provPath)
	if err != nil {
		t.Fatalf("read provenance: %v", err)
	}
	var prov struct {
		Skipped []struct {
			Method    string `json:"method"`
			RPCMethod string `json:"rpc_method"`
			RequestID string `json:"request_id"`
			Reason    string `json:"reason"`
			Block     string `json:"block"`
		} `json:"skipped"`
	}
	if err := json.Unmarshal(data, &prov); err != nil {
		t.Fatalf("unmarshal provenance: %v", err)
	}
	if len(prov.Skipped) != 1 {
		t.Fatalf("expected 1 skipped call, got %+v", prov.Skipped)
	}
	skip := prov.Skipped[0]
	want, err := RequestID("eth_getBalance", []interface{}{"0xabc", "0xffff"})
	if err != nil {
		t.Fatalf("RequestID: %v", err)
	}
	if skip.RequestID != want {
		t.Errorf("skipped request_id = %s, want %s", skip.RequestID, want)
	}
	if skip.RPCMethod != "eth_getBalance" {
		t.Errorf("skipped rpc_method = %q, want eth_getBalance", skip.RPCMethod)
	}
	if skip.Method != "eth_getBalance_high" || skip.Reason == "" || skip.Block != "0xffff" {
		t.Errorf("the existing fields must be unchanged: %+v", skip)
	}
}

// A request's identity is a property of the request, not of how the run chose
// to compare it. Strict mode changes what the block override does to an
// eth_getLogs filter carrying blockHash — so the wire differs between the two
// runs — and the identities must not move with it. If they did, an approved
// omission list frozen under one mode would stop matching under the other.
func TestRequestIdentityIsIndependentOfStrictMode(t *testing.T) {
	identitiesFor := func(strict bool) []string {
		node := newRPCFake(t, "0x1", func(req rpcRequest) interface{} { return []interface{}{} })
		cfg := &ComparisonConfig{
			Name:    "modes",
			Methods: []string{"eth_getLogs_byhash", "eth_getLogs_range", "eth_getBalance_variant1"},
			MethodRPCNames: map[string]string{
				"eth_getLogs_byhash":      "eth_getLogs",
				"eth_getLogs_range":       "eth_getLogs",
				"eth_getBalance_variant1": "eth_getBalance",
			},
			CustomParameters: map[string][]interface{}{
				"eth_getLogs_byhash":      {map[string]interface{}{"blockHash": "0xfeed"}},
				"eth_getLogs_range":       {map[string]interface{}{"fromBlock": "latest"}},
				"eth_getBalance_variant1": {"0xabc", "latest"},
			},
			BlockOverride:            "0x1406f40",
			StrictResponseComparison: strict,
			Clients: []*types.ClientConfig{
				{Name: "baseline", URL: node.URL},
				{Name: "candidate", URL: node.URL},
			},
			TimeoutSeconds: 5,
			Concurrency:    1,
			OutputDir:      t.TempDir(),
		}
		comp, err := NewComparator(cfg)
		if err != nil {
			t.Fatalf("NewComparator: %v", err)
		}
		if _, err := comp.Run(); err != nil {
			t.Fatalf("Run(strict=%v): %v", strict, err)
		}
		ids := make([]string, 0, 3)
		for _, result := range comp.GetResults() {
			ids = append(ids, result.Method+"="+result.RequestID)
		}
		sort.Strings(ids)

		// Sanity: the two modes really did behave differently on the wire, or
		// this test proves nothing. Strict leaves the blockHash filter alone,
		// so it records one transform fewer.
		transforms, _ := comp.Provenance()["wire_transformations"].([]wireTransform)
		want := 3
		if strict {
			want = 2
		}
		if len(transforms) != want {
			t.Errorf("strict=%v recorded %d transforms, want %d", strict, len(transforms), want)
		}
		return ids
	}

	defaultMode := identitiesFor(false)
	strictMode := identitiesFor(true)
	if len(defaultMode) != 3 {
		t.Fatalf("expected 3 results, got %v", defaultMode)
	}
	if !reflect.DeepEqual(defaultMode, strictMode) {
		t.Errorf("identities moved with the comparison mode:\n default %v\n strict  %v", defaultMode, strictMode)
	}
}
