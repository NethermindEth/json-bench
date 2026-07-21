package comparator

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTempYAML chdirs into a fresh temp dir and writes a file there, returning
// the relative name (SafeReadPath rejects absolute paths).
func writeTempYAML(t *testing.T, name, contents string) string {
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
	if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return name
}

func TestLoadComparisonRules_ComparisonBlock(t *testing.T) {
	path := writeTempYAML(t, "rules.yaml", `
comparison:
  block_override: "0x1406f40"
  rules:
    - path: result.totalDifficulty
      kind: ignore
    - method: eth_estimateGas
      path: result
      kind: numeric_tolerance
      rel: 0.1
`)
	rules, blockOverride, err := LoadComparisonRules(path)
	if err != nil {
		t.Fatalf("LoadComparisonRules: %v", err)
	}
	if blockOverride != "0x1406f40" {
		t.Errorf("block override = %q, want 0x1406f40", blockOverride)
	}
	if len(rules) != 2 || rules[0].Kind != RuleIgnore || rules[1].Kind != RuleNumericTolerance {
		t.Errorf("rules round-trip failed: %+v", rules)
	}
}

func TestLoadComparisonRules_RootForm(t *testing.T) {
	path := writeTempYAML(t, "rules.yaml", `
block_override: "0x10"
rules:
  - path: result.x
    kind: ignore
`)
	rules, blockOverride, err := LoadComparisonRules(path)
	if err != nil {
		t.Fatalf("LoadComparisonRules: %v", err)
	}
	if blockOverride != "0x10" || len(rules) != 1 {
		t.Errorf("root-form round-trip failed: bo=%q rules=%d", blockOverride, len(rules))
	}
}

func TestLoadComparisonRules_UnknownKind(t *testing.T) {
	path := writeTempYAML(t, "rules.yaml", `
rules:
  - path: result
    kind: nope
`)
	if _, _, err := LoadComparisonRules(path); err == nil {
		t.Fatal("expected unknown kind error")
	}
}

func TestLoadComparisonRules_Empty(t *testing.T) {
	path := writeTempYAML(t, "rules.yaml", "name: irrelevant\n")
	if _, _, err := LoadComparisonRules(path); err == nil {
		t.Fatal("expected error for a rules file with no rules or block_override")
	}
}

func TestMergeComparisonRulesPrepends(t *testing.T) {
	cfg := &ComparisonConfig{Rules: []ComparisonRule{{Path: "result.a", Kind: RuleIgnore}}}
	cfg.MergeComparisonRules([]ComparisonRule{{Path: "result.b", Kind: RuleIgnore}})
	if len(cfg.Rules) != 2 || cfg.Rules[0].Path != "result.b" || cfg.Rules[1].Path != "result.a" {
		t.Errorf("expected layered rule first, got %+v", cfg.Rules)
	}
}

// TestCorpusPlusRulesCompose is the headline #1 case: a corpus run (no config
// rules) plus a --rules file must apply those rules during comparison.
func TestCorpusPlusRulesCompose(t *testing.T) {
	dir := writeCorpus(t, map[string]string{
		"c.jsonl": `{"method":"eth_getBlockByNumber","params":["0x10",false]}` + "\n",
	})
	cfg, err := LoadCorpusConfig(dir, 0, 42, "")
	if err != nil {
		t.Fatalf("LoadCorpusConfig: %v", err)
	}
	if len(cfg.Rules) != 0 {
		t.Fatalf("corpus config should start with no rules, got %d", len(cfg.Rules))
	}

	cfg.MergeComparisonRules([]ComparisonRule{{Path: "result.totalDifficulty", Kind: RuleIgnore}})

	ctx := newDiffContext("eth_getBlockByNumber", cfg.Rules)
	r1 := map[string]interface{}{"result": map[string]interface{}{"number": "0x10", "totalDifficulty": nil}}
	r2 := map[string]interface{}{"result": map[string]interface{}{"number": "0x10", "totalDifficulty": "0xabc"}}
	diffs, err := compareJSONRPCResponses(ctx, r1, r2)
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if _, ok := diffs["result_differences"]; ok {
		t.Errorf("merged ignore rule should suppress the totalDifficulty diff, got %v", diffs)
	}
}
