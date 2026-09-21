package comparator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestDefaultCorpusExclusionsAreTheHistoricalList pins the default policy to
// the list --from-jsonl applied before it became a flag, method by method: a
// caller who never passes --corpus-exclude must see no change.
func TestDefaultCorpusExclusionsAreTheHistoricalList(t *testing.T) {
	def := DefaultCorpusExclusions()
	wantMethods := []string{"eth_blockNumber", "eth_gasPrice", "eth_getProof", "eth_maxPriorityFeePerGas", "eth_syncing"}
	if got := def.SortedMethods(); !reflect.DeepEqual(got, wantMethods) {
		t.Errorf("default methods = %v, want %v", got, wantMethods)
	}
	if !reflect.DeepEqual(def.Prefixes, []string{"debug_"}) {
		t.Errorf("default prefixes = %v, want [debug_]", def.Prefixes)
	}
	for _, method := range []string{"debug_traceCall", "debug_getRawHeader", "eth_getProof", "eth_blockNumber"} {
		if !def.Excludes(method, true) {
			t.Errorf("default should exclude %s", method)
		}
	}
	for _, method := range []string{"eth_call", "trace_block", "net_version"} {
		if def.Excludes(method, false) {
			t.Errorf("default should not exclude %s", method)
		}
	}
	if def.Excludes("eth_feeHistory", true) || !def.Excludes("eth_feeHistory", false) {
		t.Error("eth_feeHistory must follow the block override, not the list")
	}
	if def.String() != DefaultCorpusExcludeFlag {
		t.Errorf("String() = %q, want the flag default %q", def.String(), DefaultCorpusExcludeFlag)
	}
}

func TestParseCorpusExclusions(t *testing.T) {
	cases := []struct {
		value    string
		methods  []string
		prefixes []string
		wantErr  string
	}{
		{value: "debug_,eth_getProof", methods: []string{"eth_getProof"}, prefixes: []string{"debug_"}},
		{value: " trace_ , debug_ ,eth_syncing ", methods: []string{"eth_syncing"}, prefixes: []string{"debug_", "trace_"}},
		{value: "eth_getProof,eth_getProof", methods: []string{"eth_getProof"}, prefixes: nil},
		{value: "none", methods: []string{}, prefixes: nil},
		{value: " none ", methods: []string{}, prefixes: nil},
		{value: "", wantErr: "is empty"},
		{value: "   ", wantErr: "is empty"},
		{value: "debug_,", wantErr: "empty entry"},
		{value: "none,eth_getProof", wantErr: "cannot be combined"},
		{value: "_", wantErr: "names no namespace"},
		{value: "eth getProof", wantErr: "whitespace"},
	}
	for _, tc := range cases {
		got, err := ParseCorpusExclusions(tc.value)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Parse(%q): err = %v, want containing %q", tc.value, err, tc.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("Parse(%q): unexpected error %v", tc.value, err)
			continue
		}
		if !reflect.DeepEqual(got.SortedMethods(), tc.methods) {
			t.Errorf("Parse(%q): methods = %v, want %v", tc.value, got.SortedMethods(), tc.methods)
		}
		if !reflect.DeepEqual(got.Prefixes, tc.prefixes) {
			t.Errorf("Parse(%q): prefixes = %v, want %v", tc.value, got.Prefixes, tc.prefixes)
		}
	}
	none, _ := ParseCorpusExclusions("none")
	if none.String() != CorpusExcludeNone {
		t.Errorf("String() of no exclusions = %q, want %q", none.String(), CorpusExcludeNone)
	}
	if none.Excludes("debug_traceCall", true) || none.Excludes("eth_getProof", true) {
		t.Error("none must exclude nothing")
	}
	if !none.Excludes("eth_feeHistory", false) {
		t.Error("none does not lift the pinnable rule: eth_feeHistory without an override is still dropped")
	}
}

// TestLoadCorpusConfigWithExclusionsLiftsThePrefix is the case the flag exists
// for: a corpus of debug_ getters is loaded when the prefix is not excluded,
// while the method list still applies.
func TestLoadCorpusConfigWithExclusionsLiftsThePrefix(t *testing.T) {
	dir := writeCorpus(t, map[string]string{
		"debug.jsonl": `{"method":"debug_getRawHeader","params":["0x375e47"]}
{"method":"debug_getRawTransaction","params":["0x` + strings.Repeat("ab", 32) + `"]}
`,
		"excluded.jsonl": `{"method":"eth_getProof","params":["0x1",[],"0x10"]}` + "\n",
	})
	exclusions, err := ParseCorpusExclusions("eth_getProof,eth_gasPrice,eth_syncing,eth_blockNumber,eth_maxPriorityFeePerGas")
	if err != nil {
		t.Fatal(err)
	}

	// Default policy: nothing usable.
	if _, _, err := LoadCorpusConfig(dir, 0, 42, "0x375e47"); err == nil {
		t.Fatal("default policy should have excluded every call")
	}

	cfg, report, err := LoadCorpusConfigWithExclusions(dir, 0, 42, "0x375e47", exclusions)
	if err != nil {
		t.Fatalf("LoadCorpusConfigWithExclusions: %v", err)
	}
	got := map[string]int{}
	for _, id := range cfg.Methods {
		got[cfg.MethodRPCNames[id]]++
	}
	want := map[string]int{"debug_getRawHeader": 1, "debug_getRawTransaction": 1}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("loaded methods = %v, want %v", got, want)
	}
	if len(report.ExcludedRequests) != 1 || report.ExcludedRequests[0].Method != "eth_getProof" {
		t.Errorf("excluded requests = %+v, want exactly eth_getProof", report.ExcludedRequests)
	}
	if report.Excluded != 1 {
		t.Errorf("files held only excluded methods = %d, want 1", report.Excluded)
	}
}

func TestLoadCorpusConfigWithExclusionsNoneKeepsEverythingButUnpinnedFeeHistory(t *testing.T) {
	dir := writeCorpus(t, map[string]string{
		"all.jsonl": `{"method":"eth_getProof","params":["0x1",[],"0x10"]}
{"method":"eth_blockNumber","params":[]}
{"method":"debug_traceCall","params":[{},"0x10"]}
{"method":"eth_feeHistory","params":["0x5","latest",[]]}
`,
	})
	none, _ := ParseCorpusExclusions(CorpusExcludeNone)
	cfg, report, err := LoadCorpusConfigWithExclusions(dir, 0, 42, "", none)
	if err != nil {
		t.Fatalf("LoadCorpusConfigWithExclusions: %v", err)
	}
	if len(cfg.Methods) != 3 {
		t.Errorf("expected 3 calls loaded (eth_feeHistory dropped without an override), got %v", cfg.Methods)
	}
	if len(report.ExcludedRequests) != 1 || report.ExcludedRequests[0].Method != "eth_feeHistory" {
		t.Errorf("excluded = %+v, want exactly the unpinned eth_feeHistory", report.ExcludedRequests)
	}
}

// TestCorpusLoadReportRecordsThePolicy: a consumer reconciling the run reads
// the effective exclusions from the report, so the report must carry them —
// and the default run must say so explicitly rather than by omission.
func TestCorpusLoadReportRecordsThePolicy(t *testing.T) {
	dir := writeCorpus(t, map[string]string{
		"c.jsonl": `{"method":"eth_call","params":[{"to":"0x1"},"0x10"]}` + "\n",
	})
	_, report, err := LoadCorpusConfig(dir, 0, 42, "0x10")
	if err != nil {
		t.Fatal(err)
	}
	doc := NewCorpusLoadReportDocument(dir, report)
	want := CorpusPolicy{
		ExcludedMethods:  []string{"eth_blockNumber", "eth_gasPrice", "eth_getProof", "eth_maxPriorityFeePerGas", "eth_syncing"},
		ExcludedPrefixes: []string{"debug_"},
		PinnableMethods:  []string{"eth_feeHistory"},
		PinnableKept:     true,
	}
	if !reflect.DeepEqual(doc.Policy, want) {
		t.Errorf("policy = %+v, want %+v", doc.Policy, want)
	}
	if doc.SchemaVersion != 1 {
		t.Errorf("schema_version = %d; adding policy is additive and must not bump it", doc.SchemaVersion)
	}

	none, _ := ParseCorpusExclusions(CorpusExcludeNone)
	_, report, err = LoadCorpusConfigWithExclusions(dir, 0, 42, "", none)
	if err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	if err := SaveCorpusLoadReport(outDir, dir, report); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(outDir, CorpusLoadReportFilename))
	if err != nil {
		t.Fatal(err)
	}
	var onDisk map[string]json.RawMessage
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	var policy map[string]interface{}
	if err := json.Unmarshal(onDisk["policy"], &policy); err != nil {
		t.Fatalf("policy block: %v", err)
	}
	// Empty lists serialize as [] rather than null, so a consumer reading the
	// list sees "nothing excluded", not "field absent".
	if got := string(onDisk["policy"]); !strings.Contains(got, `"excluded_methods": []`) || !strings.Contains(got, `"excluded_prefixes": []`) {
		t.Errorf("policy on disk = %s, want empty arrays", got)
	}
	if policy["pinnable_kept"] != false {
		t.Errorf("pinnable_kept = %v, want false without an override", policy["pinnable_kept"])
	}
}

// TestCorpusLoadReportFromNilReportHasAnEmptyPolicy: no load ran, so no policy
// was applied, and the lists are present but empty.
func TestCorpusLoadReportFromNilReportHasAnEmptyPolicy(t *testing.T) {
	doc := NewCorpusLoadReportDocument("/nowhere", nil)
	if len(doc.Policy.ExcludedMethods) != 0 || len(doc.Policy.ExcludedPrefixes) != 0 || doc.Policy.PinnableKept {
		t.Errorf("policy from nil report = %+v, want empty", doc.Policy)
	}
	if doc.Policy.ExcludedMethods == nil || doc.Policy.ExcludedPrefixes == nil || doc.Policy.PinnableMethods == nil {
		t.Error("policy lists must be empty arrays, not null")
	}
}
