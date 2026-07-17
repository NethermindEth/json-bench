package comparator

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// writeCorpus chdirs into a temp dir, writes the given files under a "corpus"
// subdir, and returns the relative dir (SafeReadPath rejects absolute paths).
func writeCorpus(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	corpusDir := "corpus"
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

	cfg, err := LoadCorpusConfig(dir, 0, 42, "")
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

	cfg, err := LoadCorpusConfig(dir, 0, 42, "")
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
	if _, err := LoadCorpusConfig(dir, 0, 42, ""); err == nil {
		t.Error("expected eth_feeHistory to be excluded without a block override")
	}

	// With a block override it is pinnable and kept.
	cfg, err := LoadCorpusConfig(dir, 0, 42, "0x1406f40")
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

	cfg, err := LoadCorpusConfig(dir, 3, 42, "")
	if err != nil {
		t.Fatalf("LoadCorpusConfig: %v", err)
	}
	if len(cfg.Methods) != 3 {
		t.Fatalf("expected sample cap of 3, got %d", len(cfg.Methods))
	}

	// Sampling is deterministic for a fixed seed.
	cfg2, _ := LoadCorpusConfig(dir, 3, 42, "")
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
	if _, err := LoadCorpusConfig(dir, 0, 42, ""); err == nil {
		t.Error("expected error when corpus is empty after exclusions")
	}
}
