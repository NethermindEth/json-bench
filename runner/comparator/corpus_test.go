package comparator

import (
	"fmt"
	"os"
	"path/filepath"
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
