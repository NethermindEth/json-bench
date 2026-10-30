package comparator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeCorpus writes the given files under a "corpus" subdir of a temp dir and
// returns its absolute path.
func writeCorpus(t *testing.T, files map[string]string) string {
	t.Helper()
	corpusDir := filepath.Join(t.TempDir(), "corpus")
	if err := os.Mkdir(corpusDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(corpusDir, name), []byte(contents), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return corpusDir
}

func TestLoadCorpusConfig(t *testing.T) {
	dir := writeCorpus(t, map[string]string{
		"eth_call-mainnet.jsonl": `{"method":"eth_call","params":[{"to":"0x1"},"0x10"]}
{"method":"eth_call","params":[{"to":"0x2"},"0x11"]}
{"method":"eth_call","params":[{"to":"0x3"},"0x12"]}
`,
		"excluded.jsonl": `{"method":"eth_getProof","params":[]}
{"method":"eth_blockNumber","params":[]}
{"method":"debug_traceCall","params":[]}
`,
	})

	cfg, _, err := LoadCorpusConfig(dir, 0, 42, "")
	if err != nil {
		t.Fatalf("LoadCorpusConfig: %v", err)
	}
	if len(cfg.Methods) != 3 {
		t.Fatalf("expected 3 eth_call variants, got %d (%v)", len(cfg.Methods), cfg.Methods)
	}
	for _, id := range cfg.Methods {
		if cfg.MethodRPCNames[id] != "eth_call" {
			t.Errorf("identifier %q maps to %q, want eth_call", id, cfg.MethodRPCNames[id])
		}
	}
}

func TestLoadCorpusConfigRecursesAndReadsJSON(t *testing.T) {
	dir := writeCorpus(t, map[string]string{
		"top.jsonl":  `{"method":"eth_getCode","params":["0xabc","0x10"]}` + "\n",
		"array.json": `[{"method":"eth_call","params":[{"to":"0x1"},"0x10"]},{"method":"eth_call","params":[{"to":"0x2"},"0x11"]}]`,
	})
	// A nested subdirectory should be walked too.
	if err := os.MkdirAll(filepath.Join(dir, "contracts"), 0o755); err != nil {
		t.Fatalf("mkdir contracts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "contracts", "weth.jsonl"), []byte(`{"method":"eth_getStorageAt","params":["0xabc","0x0","0x10"]}`+"\n"), 0o600); err != nil {
		t.Fatalf("write nested: %v", err)
	}

	cfg, _, err := LoadCorpusConfig(dir, 0, 42, "")
	if err != nil {
		t.Fatalf("LoadCorpusConfig: %v", err)
	}
	methods := map[string]int{}
	for _, id := range cfg.Methods {
		methods[cfg.MethodRPCNames[id]]++
	}
	if methods["eth_getCode"] != 1 {
		t.Errorf("expected 1 eth_getCode from top-level .jsonl, got %d", methods["eth_getCode"])
	}
	if methods["eth_call"] != 2 {
		t.Errorf("expected 2 eth_call from .json array, got %d", methods["eth_call"])
	}
	if methods["eth_getStorageAt"] != 1 {
		t.Errorf("expected 1 eth_getStorageAt from nested subdir, got %d", methods["eth_getStorageAt"])
	}
}

func TestLoadCorpusConfigFeeHistoryPinning(t *testing.T) {
	dir := writeCorpus(t, map[string]string{
		"fee.jsonl": `{"method":"eth_feeHistory","params":["0x5","latest",[]]}` + "\n",
	})

	// Without a block override, eth_feeHistory is head-dependent and excluded,
	// leaving an empty corpus.
	if _, _, err := LoadCorpusConfig(dir, 0, 42, ""); err == nil {
		t.Error("expected eth_feeHistory to be excluded without a block override")
	}

	// With a block override it is pinnable and kept.
	cfg, _, err := LoadCorpusConfig(dir, 0, 42, "0x1406f40")
	if err != nil {
		t.Fatalf("LoadCorpusConfig with block override: %v", err)
	}
	if len(cfg.Methods) != 1 || cfg.MethodRPCNames[cfg.Methods[0]] != "eth_feeHistory" {
		t.Errorf("expected eth_feeHistory kept when pinned, got %v", cfg.Methods)
	}
}

func TestLoadCorpusConfigSampling(t *testing.T) {
	lines := ""
	for i := 0; i < 10; i++ {
		lines += `{"method":"eth_getBalance","params":["0xabc","0x` + string(rune('0'+i)) + `"]}` + "\n"
	}
	dir := writeCorpus(t, map[string]string{"eth_getBalance.jsonl": lines})

	cfg, _, err := LoadCorpusConfig(dir, 3, 42, "")
	if err != nil {
		t.Fatalf("LoadCorpusConfig: %v", err)
	}
	if len(cfg.Methods) != 3 {
		t.Fatalf("expected sample cap of 3, got %d", len(cfg.Methods))
	}

	// Sampling is deterministic for a fixed seed.
	cfg2, _, _ := LoadCorpusConfig(dir, 3, 42, "")
	for i, id := range cfg.Methods {
		a := fmt.Sprintf("%v", cfg.CustomParameters[id])
		b := fmt.Sprintf("%v", cfg2.CustomParameters[cfg2.Methods[i]])
		if a != b {
			t.Errorf("sampling not deterministic at %d: %s vs %s", i, a, b)
		}
	}
}

func TestLoadCorpusConfigAllExcluded(t *testing.T) {
	dir := writeCorpus(t, map[string]string{"only.jsonl": `{"method":"eth_getProof","params":[]}` + "\n"})
	if _, _, err := LoadCorpusConfig(dir, 0, 42, ""); err == nil {
		t.Error("expected error when corpus is empty after exclusions")
	}
}

// TestLoadCorpusConfigSkipsNonCorpusFiles pins the behaviour that makes
// --from-jsonl usable against a real tree: generator inputs living beside the
// corpus are skipped and reported, not fatal.
func TestLoadCorpusConfigSkipsNonCorpusFiles(t *testing.T) {
	dir := writeCorpus(t, map[string]string{
		"good.jsonl":          `{"method":"eth_call","params":[{"to":"0x1"},"0x10"]}` + "\n",
		"filter-queries.json": `[[{"fromBlock":0,"toBlock":10,"address":["0x1"]}]]`,
		"block-numbers.json":  `{"blockNumbers":[1,2,3]}`,
		"no-method.json":      `[{"fromBlock":"0x1"}]`,
	})

	cfg, report, err := LoadCorpusConfig(dir, 0, 42, "")
	if err != nil {
		t.Fatalf("LoadCorpusConfig: %v", err)
	}
	if len(cfg.Methods) != 1 {
		t.Fatalf("expected the one valid call to load, got %v", cfg.Methods)
	}
	if report.Files != 1 || report.Entries != 1 {
		t.Errorf("report = %d files / %d entries, want 1/1", report.Files, report.Entries)
	}
	if report.Excluded != 0 {
		t.Errorf("no file here is excluded-by-policy, got %d", report.Excluded)
	}
	if len(report.Skips) != 3 {
		t.Fatalf("expected 3 skips, got %d: %+v", len(report.Skips), report.Skips)
	}
	for _, skip := range report.Skips {
		if skip.Path == "" || skip.Reason == "" {
			t.Errorf("skip is missing path or reason: %+v", skip)
		}
		if filepath.Base(skip.Path) == "good.jsonl" {
			t.Error("the valid corpus file must not be skipped")
		}
	}
}

// TestLoadCorpusConfigAbsoluteDir covers the operator passing an absolute
// --from-jsonl path, which the old path guard rejected outright.
func TestLoadCorpusConfigAbsoluteDir(t *testing.T) {
	dir := writeCorpus(t, map[string]string{
		"c.jsonl": `{"method":"eth_getBalance","params":["0x1","0x10"]}` + "\n",
	})
	if !filepath.IsAbs(dir) {
		t.Fatalf("fixture dir should be absolute, got %q", dir)
	}
	cfg, report, err := LoadCorpusConfig(dir, 0, 42, "")
	if err != nil {
		t.Fatalf("LoadCorpusConfig with an absolute dir: %v", err)
	}
	if len(cfg.Methods) != 1 || report.Entries != 1 {
		t.Errorf("expected one loaded call, got %v (%d entries)", cfg.Methods, report.Entries)
	}
}

// TestLoadCorpusConfigAllSkippedReportsSkips keeps a wholly mis-shaped corpus a
// hard error, and keeps the skips visible in the report.
func TestLoadCorpusConfigAllSkippedReportsSkips(t *testing.T) {
	dir := writeCorpus(t, map[string]string{"bad.json": `[[{"fromBlock":0}]]`})
	_, report, err := LoadCorpusConfig(dir, 0, 42, "")
	if err == nil {
		t.Fatal("expected an error when nothing usable loaded")
	}
	if report == nil || len(report.Skips) != 1 {
		t.Fatalf("expected the report to carry the skip, got %+v", report)
	}
}

// A file holding only excluded methods (debug_, eth_getProof, head-dependent
// zero-arg calls) is expected, not a problem, so it is counted apart from skips.
func TestLoadCorpusConfigExcludedFileIsNotASkip(t *testing.T) {
	dir := writeCorpus(t, map[string]string{
		"good.jsonl":   `{"method":"eth_call","params":[{"to":"0x1"},"0x10"]}` + "\n",
		"traces.jsonl": `{"method":"debug_traceTransaction","params":["0x1"]}` + "\n",
	})
	cfg, report, err := LoadCorpusConfig(dir, 0, 42, "")
	if err != nil {
		t.Fatalf("LoadCorpusConfig: %v", err)
	}
	if len(cfg.Methods) != 1 {
		t.Fatalf("expected only the eth_call to load, got %v", cfg.Methods)
	}
	if report.Excluded != 1 {
		t.Errorf("Excluded = %d, want 1", report.Excluded)
	}
	if len(report.Skips) != 0 {
		t.Errorf("an excluded-methods file must not warn, got %+v", report.Skips)
	}
}

// WalkDir does not follow a symlinked root, so a linked corpus directory used to
// look empty. The root is resolved before walking.
func TestLoadCorpusConfigSymlinkedDir(t *testing.T) {
	dir := writeCorpus(t, map[string]string{
		"c.jsonl": `{"method":"eth_getBalance","params":["0x1","0x10"]}` + "\n",
	})
	link := filepath.Join(filepath.Dir(dir), "corpus-link")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	cfg, report, err := LoadCorpusConfig(link, 0, 42, "")
	if err != nil {
		t.Fatalf("LoadCorpusConfig through a symlink: %v", err)
	}
	if len(cfg.Methods) != 1 || report.Entries != 1 {
		t.Errorf("expected one loaded call, got %v (%d entries)", cfg.Methods, report.Entries)
	}
}

// findRequest returns the entry for a method in a bucket, so a test can name
// what it is looking for rather than index into a slice.
func findRequest(bucket []CorpusRequest, method string) (CorpusRequest, bool) {
	for _, request := range bucket {
		if request.Method == method {
			return request, true
		}
	}
	return CorpusRequest{}, false
}

// The load report is the machine-readable twin of the log line: every file the
// loader touched is accounted for as loaded, excluded-only or skipped, and
// every call it saw is named by identity.
func TestCorpusLoadReportAccountsForEveryFileAndCall(t *testing.T) {
	dir := writeCorpus(t, map[string]string{
		"good.jsonl": `{"method":"eth_call","params":[{"to":"0x1"},"0x10"]}
{"method":"eth_getProof","params":["0x1",[],"0x10"]}
`,
		"traces.jsonl":        `{"method":"debug_traceTransaction","params":["0x1"]}` + "\n",
		"filter-queries.json": `[[{"fromBlock":0,"toBlock":10}]]`,
		"no-method.json":      `[{"fromBlock":"0x1"}]`,
	})
	// A nested directory is walked, and shows up in the report under its path.
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "more.jsonl"),
		[]byte(`{"method":"eth_getCode","params":["0xabc","0x10"]}`+"\n"), 0o600); err != nil {
		t.Fatalf("write nested: %v", err)
	}

	_, report, err := LoadCorpusConfig(dir, 0, 42, "")
	if err != nil {
		t.Fatalf("LoadCorpusConfig: %v", err)
	}
	doc := NewCorpusLoadReportDocument(dir, report)

	if doc.SchemaVersion != CorpusLoadReportSchemaVersion {
		t.Errorf("schema_version = %d, want %d", doc.SchemaVersion, CorpusLoadReportSchemaVersion)
	}
	if doc.CorpusDir != dir {
		t.Errorf("corpus_dir = %q, want %q", doc.CorpusDir, dir)
	}

	totals := doc.Totals
	if totals.FilesScanned != 5 {
		t.Errorf("files_scanned = %d, want 5", totals.FilesScanned)
	}
	if totals.FilesLoaded != 2 {
		t.Errorf("files_loaded = %d, want 2 (good.jsonl, nested/more.jsonl)", totals.FilesLoaded)
	}
	if totals.FilesExcludedOnly != 1 {
		t.Errorf("files_excluded_only = %d, want 1 (traces.jsonl)", totals.FilesExcludedOnly)
	}
	if totals.FilesSkipped != 2 {
		t.Errorf("files_skipped = %d, want 2", totals.FilesSkipped)
	}
	if got := totals.FilesLoaded + totals.FilesExcludedOnly + totals.FilesSkipped; got != totals.FilesScanned {
		t.Errorf("the file split does not add up: %d != %d", got, totals.FilesScanned)
	}

	if totals.EntriesLoaded != 2 {
		t.Errorf("entries_loaded = %d, want 2 (eth_call, eth_getCode)", totals.EntriesLoaded)
	}
	if totals.EntriesUnnamed != 1 {
		t.Errorf("entries_unnamed = %d, want 1 (the entry in no-method.json)", totals.EntriesUnnamed)
	}
	if totals.EntriesExcluded != 2 {
		t.Errorf("entries_excluded = %d, want 2 (eth_getProof, debug_traceTransaction)", totals.EntriesExcluded)
	}
	if totals.CallsSelected != 2 {
		t.Errorf("calls_selected = %d, want 2", totals.CallsSelected)
	}
	if totals.CallsSampleDropped != 0 {
		t.Errorf("calls_sample_dropped = %d, want 0 without --sample", totals.CallsSampleDropped)
	}
	if totals.IdentityErrors != 0 {
		t.Errorf("identity_errors = %d, want 0", totals.IdentityErrors)
	}

	// Per-file counts: the loaded file's two entries split one/one.
	byPath := map[string]CorpusFileReport{}
	for _, file := range doc.Files {
		byPath[filepath.Base(file.Path)] = file
	}
	// Four of the five parsed: only filter-queries.json failed to parse at all.
	// no-method.json is here with its entries counted as unnamed, and also in
	// skipped_files — the two answer different questions.
	if len(byPath) != 4 {
		t.Fatalf("expected 4 parsed files in the report, got %d: %+v", len(byPath), doc.Files)
	}
	if nm := byPath["no-method.json"]; nm.Entries != 1 || nm.Unnamed != 1 || nm.Loaded != 0 {
		t.Errorf("no-method.json = %+v, want 1 entry / 1 unnamed / 0 loaded", nm)
	}
	if good := byPath["good.jsonl"]; good.Entries != 2 || good.Loaded != 1 || good.Excluded != 1 {
		t.Errorf("good.jsonl = %+v, want 2 entries / 1 loaded / 1 excluded", good)
	}
	if _, ok := byPath["more.jsonl"]; !ok {
		t.Error("a nested corpus file must appear in the report")
	}

	// Skipped files carry a path and a reason, and the valid ones are not there.
	if len(doc.SkippedFiles) != 2 {
		t.Fatalf("expected 2 skipped files, got %+v", doc.SkippedFiles)
	}
	for _, skip := range doc.SkippedFiles {
		if skip.Path == "" || skip.Reason == "" {
			t.Errorf("skip missing path or reason: %+v", skip)
		}
		if filepath.Base(skip.Path) == "good.jsonl" {
			t.Error("a valid corpus file must not be skipped")
		}
	}

	// Every call is named by identity, in the bucket that explains its fate.
	selected, ok := findRequest(doc.Requests.Selected, "eth_call")
	if !ok {
		t.Fatalf("eth_call missing from selected: %+v", doc.Requests.Selected)
	}
	want, err := RequestID("eth_call", []interface{}{map[string]interface{}{"to": "0x1"}, "0x10"})
	if err != nil {
		t.Fatalf("RequestID: %v", err)
	}
	if selected.RequestID != want {
		t.Errorf("selected eth_call request_id = %s, want %s", selected.RequestID, want)
	}
	if selected.CallID != "eth_call_variant1" {
		t.Errorf("selected eth_call call_id = %q, want eth_call_variant1", selected.CallID)
	}
	if filepath.Base(selected.Path) != "good.jsonl" {
		t.Errorf("selected eth_call path = %q, want good.jsonl", selected.Path)
	}
	for _, method := range []string{"eth_getProof", "debug_traceTransaction"} {
		excluded, ok := findRequest(doc.Requests.Excluded, method)
		if !ok {
			t.Errorf("%s missing from excluded: %+v", method, doc.Requests.Excluded)
			continue
		}
		if len(excluded.RequestID) != 64 {
			t.Errorf("%s excluded request_id = %q, want a 64-character digest", method, excluded.RequestID)
		}
		if excluded.CallID != "" {
			t.Errorf("%s was never selected, so it has no call_id: %q", method, excluded.CallID)
		}
	}
}

// A call --sample did not choose is an omission like any other, and is listed
// so a consumer can approve it rather than discover a short population.
func TestCorpusLoadReportAccountsForSampleDrops(t *testing.T) {
	lines := ""
	for i := 0; i < 10; i++ {
		lines += `{"method":"eth_getBalance","params":["0xabc","0x` + string(rune('0'+i)) + `"]}` + "\n"
	}
	dir := writeCorpus(t, map[string]string{"eth_getBalance.jsonl": lines})

	_, report, err := LoadCorpusConfig(dir, 3, 42, "")
	if err != nil {
		t.Fatalf("LoadCorpusConfig: %v", err)
	}
	doc := NewCorpusLoadReportDocument(dir, report)

	if doc.Sampling.Sample != 3 || doc.Sampling.Seed != 42 {
		t.Errorf("sampling = %+v, want sample 3 seed 42", doc.Sampling)
	}
	if doc.Totals.EntriesLoaded != 10 {
		t.Errorf("entries_loaded = %d, want all 10", doc.Totals.EntriesLoaded)
	}
	if doc.Totals.CallsSelected != 3 || doc.Totals.CallsSampleDropped != 7 {
		t.Fatalf("selected/dropped = %d/%d, want 3/7", doc.Totals.CallsSelected, doc.Totals.CallsSampleDropped)
	}

	// The two buckets partition the ten calls: no identity is in both, and
	// together they are the whole corpus.
	seen := map[string]int{}
	for _, request := range append(append([]CorpusRequest{}, doc.Requests.Selected...), doc.Requests.SampleDropped...) {
		seen[request.RequestID]++
	}
	if len(seen) != 10 {
		t.Errorf("selected and dropped cover %d distinct requests, want 10", len(seen))
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("request %s appears %d times across the two buckets", id, count)
		}
	}
}

// A corpus that yields nothing is the case a fail-closed consumer most needs
// the accounting for, so the report survives the error.
func TestCorpusLoadReportSurvivesAFailedLoad(t *testing.T) {
	dir := writeCorpus(t, map[string]string{
		"only.jsonl": `{"method":"eth_getProof","params":[]}` + "\n",
		"bad.json":   `[[{"fromBlock":0}]]`,
	})
	_, report, err := LoadCorpusConfig(dir, 0, 42, "")
	if err == nil {
		t.Fatal("expected an error when nothing usable loaded")
	}
	doc := NewCorpusLoadReportDocument(dir, report)
	if doc.Totals.FilesScanned != 2 || doc.Totals.FilesSkipped != 1 || doc.Totals.FilesExcludedOnly != 1 {
		t.Errorf("totals = %+v, want 2 scanned / 1 skipped / 1 excluded-only", doc.Totals)
	}
	if len(doc.Requests.Excluded) != 1 {
		t.Errorf("the excluded call must still be named: %+v", doc.Requests.Excluded)
	}
	if len(doc.Requests.Selected) != 0 {
		t.Errorf("nothing was selected, got %+v", doc.Requests.Selected)
	}
}

// A nil report means the corpus directory could not be read at all. The
// document is still produced, so "no report" never means "no corpus problem".
func TestCorpusLoadReportFromNilReport(t *testing.T) {
	doc := NewCorpusLoadReportDocument("/nowhere", nil)
	if doc.SchemaVersion != CorpusLoadReportSchemaVersion || doc.CorpusDir != "/nowhere" {
		t.Errorf("doc = %+v", doc)
	}
	if doc.Totals.FilesScanned != 0 {
		t.Errorf("files_scanned = %d, want 0", doc.Totals.FilesScanned)
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Empty buckets serialize as [] rather than null, so a consumer can iterate
	// without a nil check.
	for _, key := range []string{`"selected":[]`, `"excluded":[]`, `"sample_dropped":[]`, `"files":[]`, `"skipped_files":[]`} {
		if !strings.Contains(string(encoded), key) {
			t.Errorf("expected %s in %s", key, encoded)
		}
	}
}

// SaveCorpusLoadReport writes valid JSON at the documented name, creating the
// output directory if it does not exist.
func TestSaveCorpusLoadReport(t *testing.T) {
	dir := writeCorpus(t, map[string]string{
		"c.jsonl": `{"method":"eth_getBalance","params":["0x1","0x10"]}` + "\n",
	})
	_, report, err := LoadCorpusConfig(dir, 0, 42, "")
	if err != nil {
		t.Fatalf("LoadCorpusConfig: %v", err)
	}
	out := filepath.Join(t.TempDir(), "outputs", "nested")
	if err := SaveCorpusLoadReport(out, dir, report); err != nil {
		t.Fatalf("SaveCorpusLoadReport: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(out, CorpusLoadReportFilename))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var doc CorpusLoadReportDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("the report is not valid JSON: %v", err)
	}
	if doc.Totals.CallsSelected != 1 || len(doc.Requests.Selected) != 1 {
		t.Errorf("round-tripped doc = %+v", doc.Totals)
	}
	if doc.Identity.Algorithm != "sha256" {
		t.Errorf("identity.algorithm = %q, want sha256", doc.Identity.Algorithm)
	}
}

// A numeric param keeps the digits the corpus recorded. Through float64 the
// value below becomes 9007199254740992 — a different request, sent to a
// different block, under an identity that matches nothing upstream.
func TestCorpusLoadPreservesLargeNumbers(t *testing.T) {
	dir := writeCorpus(t, map[string]string{
		"big.jsonl": `{"method":"eth_getBalance","params":["0x1",9007199254740993]}` + "\n",
		"arr.json":  `[{"method":"eth_getCode","params":["0x2",9007199254740993]}]`,
	})
	cfg, report, err := LoadCorpusConfig(dir, 0, 42, "")
	if err != nil {
		t.Fatalf("LoadCorpusConfig: %v", err)
	}
	for _, id := range cfg.Methods {
		encoded, err := json.Marshal(cfg.CustomParameters[id])
		if err != nil {
			t.Fatalf("marshal params of %s: %v", id, err)
		}
		if !strings.Contains(string(encoded), "9007199254740993") {
			t.Errorf("%s params round-tripped to %s", id, encoded)
		}
	}
	for _, request := range report.Selected {
		want, err := RequestID(request.Method, cfg.CustomParameters[request.CallID])
		if err != nil {
			t.Fatalf("RequestID: %v", err)
		}
		if request.RequestID != want {
			t.Errorf("%s request_id = %s, want %s", request.CallID, request.RequestID, want)
		}
	}
}

// Trailing data after a top-level array is a malformed corpus file, not a
// partially usable one.
func TestCorpusLoadRejectsTrailingData(t *testing.T) {
	dir := writeCorpus(t, map[string]string{
		"good.jsonl":    `{"method":"eth_call","params":[{"to":"0x1"},"0x10"]}` + "\n",
		"trailing.json": `[{"method":"eth_call","params":[]}] {"method":"eth_call"}`,
	})
	_, report, err := LoadCorpusConfig(dir, 0, 42, "")
	if err != nil {
		t.Fatalf("LoadCorpusConfig: %v", err)
	}
	if len(report.Skips) != 1 || filepath.Base(report.Skips[0].Path) != "trailing.json" {
		t.Fatalf("expected trailing.json to be skipped, got %+v", report.Skips)
	}
}

// The report's whole value is that its numbers reconcile, so the invariant it
// documents is asserted directly rather than inferred from a fixture's counts.
// The corpus below is deliberately awkward: a good file, a file of entries that
// name no method, a file of only excluded methods, one that does not parse, and
// sampling dropping calls from the good one.
func TestCorpusLoadReportTotalsAddUp(t *testing.T) {
	lines := ""
	for i := 0; i < 6; i++ {
		lines += `{"method":"eth_getBalance","params":["0xabc","0x` + string(rune('0'+i)) + `"]}` + "\n"
	}
	dir := writeCorpus(t, map[string]string{
		"good.jsonl":      lines,
		"no-method.json":  `[{"fromBlock":"0x1"},{"fromBlock":"0x2"},{"fromBlock":"0x3"}]`,
		"excluded.jsonl":  `{"method":"eth_getProof","params":[]}` + "\n" + `{"method":"debug_traceCall","params":[]}` + "\n",
		"unparsable.json": `[[{"fromBlock":0}]]`,
	})

	_, report, err := LoadCorpusConfig(dir, 2, 42, "")
	if err != nil {
		t.Fatalf("LoadCorpusConfig: %v", err)
	}
	totals := NewCorpusLoadReportDocument(dir, report).Totals

	if got := totals.FilesLoaded + totals.FilesExcludedOnly + totals.FilesSkipped; got != totals.FilesScanned {
		t.Errorf("files: %d loaded + %d excluded-only + %d skipped = %d, want files_scanned %d",
			totals.FilesLoaded, totals.FilesExcludedOnly, totals.FilesSkipped, got, totals.FilesScanned)
	}
	if got := totals.EntriesUnnamed + totals.EntriesExcluded + totals.EntriesLoaded; got != totals.EntriesParsed {
		t.Errorf("entries: %d unnamed + %d excluded + %d loaded = %d, want entries_parsed %d",
			totals.EntriesUnnamed, totals.EntriesExcluded, totals.EntriesLoaded, got, totals.EntriesParsed)
	}
	if got := totals.CallsSelected + totals.CallsSampleDropped; got != totals.EntriesLoaded {
		t.Errorf("calls: %d selected + %d sample-dropped = %d, want entries_loaded %d",
			totals.CallsSelected, totals.CallsSampleDropped, got, totals.EntriesLoaded)
	}

	// And the fixture really does exercise every bucket, or the sums above
	// would be a tautology over zeroes.
	for name, got := range map[string]int{
		"files_scanned":        totals.FilesScanned,
		"files_skipped":        totals.FilesSkipped,
		"files_excluded_only":  totals.FilesExcludedOnly,
		"entries_unnamed":      totals.EntriesUnnamed,
		"entries_excluded":     totals.EntriesExcluded,
		"calls_sample_dropped": totals.CallsSampleDropped,
	} {
		if got == 0 {
			t.Errorf("%s is 0: the fixture does not exercise that bucket", name)
		}
	}
}
