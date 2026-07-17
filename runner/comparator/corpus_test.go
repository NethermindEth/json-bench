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

	cfg, err := LoadCorpusConfig(dir, 0, 42)
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

func TestLoadCorpusConfigSampling(t *testing.T) {
	lines := ""
	for i := 0; i < 10; i++ {
		lines += `{"method":"eth_getBalance","params":["0xabc","0x` + string(rune('0'+i)) + `"]}` + "\n"
	}
	dir := writeCorpus(t, map[string]string{"eth_getBalance.jsonl": lines})

	cfg, err := LoadCorpusConfig(dir, 3, 42)
	if err != nil {
		t.Fatalf("LoadCorpusConfig: %v", err)
	}
	if len(cfg.Methods) != 3 {
		t.Fatalf("expected sample cap of 3, got %d", len(cfg.Methods))
	}

	// Sampling is deterministic for a fixed seed.
	cfg2, _ := LoadCorpusConfig(dir, 3, 42)
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
	if _, err := LoadCorpusConfig(dir, 0, 42); err == nil {
		t.Error("expected error when corpus is empty after exclusions")
	}
}
