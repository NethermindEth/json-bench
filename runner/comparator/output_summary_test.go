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
