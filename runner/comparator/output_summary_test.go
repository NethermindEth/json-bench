package comparator

import (
	"strings"
	"testing"
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
