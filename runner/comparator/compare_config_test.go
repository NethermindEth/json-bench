package comparator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeCompareFixture writes contents to a temp file inside a fresh working
// directory and returns the relative filename to feed LoadCompareConfig,
// which rejects absolute paths via config.SafeReadPath.
func writeCompareFixture(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prev)
	})
	name := "compare.yaml"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return name
}

func TestLoadCompareConfig_Valid(t *testing.T) {
	path := writeCompareFixture(t, `
name: "cross-client sanity"
description: "mixed shapes"
calls:
  eth_blockNumber:
    - params: []
  eth_getBalance:
    - id: "vitalik-latest"
      params: ["0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045", "latest"]
    - params: ["0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045", "0x1000"]
  eth_call:
    - params:
        - to: "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2"
          data: "0x70a08231000000000000000000000000000000000000000000000000000000000000000a"
        - "latest"
`)

	cfg, err := LoadCompareConfig(path)
	if err != nil {
		t.Fatalf("LoadCompareConfig: %v", err)
	}
	if cfg.Name != "cross-client sanity" {
		t.Fatalf("unexpected Name: %q", cfg.Name)
	}
	if cfg.Description != "mixed shapes" {
		t.Fatalf("unexpected Description: %q", cfg.Description)
	}

	want := []string{
		"eth_blockNumber_variant1",
		"eth_getBalance_vitalik-latest",
		"eth_getBalance_variant2",
		"eth_call_variant1",
	}
	if len(cfg.Methods) != len(want) {
		t.Fatalf("Methods len = %d, want %d (%v)", len(cfg.Methods), len(want), cfg.Methods)
	}
	for i, id := range want {
		if cfg.Methods[i] != id {
			t.Errorf("Methods[%d] = %q, want %q", i, cfg.Methods[i], id)
		}
	}

	// MethodRPCNames must strip the suffix back to the wire-level method.
	rpcExpect := map[string]string{
		"eth_blockNumber_variant1":      "eth_blockNumber",
		"eth_getBalance_vitalik-latest": "eth_getBalance",
		"eth_getBalance_variant2":       "eth_getBalance",
		"eth_call_variant1":             "eth_call",
	}
	for id, wantRPC := range rpcExpect {
		if got := cfg.MethodRPCNames[id]; got != wantRPC {
			t.Errorf("MethodRPCNames[%q] = %q, want %q", id, got, wantRPC)
		}
	}

	// CustomParameters must round-trip the params from YAML.
	if len(cfg.CustomParameters["eth_blockNumber_variant1"]) != 0 {
		t.Errorf("eth_blockNumber params should be empty, got %v", cfg.CustomParameters["eth_blockNumber_variant1"])
	}
	balanceParams := cfg.CustomParameters["eth_getBalance_vitalik-latest"]
	if len(balanceParams) != 2 || balanceParams[0] != "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045" || balanceParams[1] != "latest" {
		t.Errorf("eth_getBalance vitalik-latest params round-trip failed: %v", balanceParams)
	}
	callParams := cfg.CustomParameters["eth_call_variant1"]
	if len(callParams) != 2 {
		t.Fatalf("eth_call params len = %d, want 2 (%v)", len(callParams), callParams)
	}
	obj, ok := callParams[0].(map[string]interface{})
	if !ok {
		t.Fatalf("eth_call first param should be map, got %T", callParams[0])
	}
	if obj["to"] != "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2" {
		t.Errorf("eth_call.to round-trip failed: %v", obj["to"])
	}

	// Fields the caller is expected to populate must be zero-valued.
	if cfg.Concurrency != 0 || cfg.TimeoutSeconds != 0 || len(cfg.Clients) != 0 {
		t.Errorf("caller-owned fields should be zero, got concurrency=%d timeout=%d clients=%d", cfg.Concurrency, cfg.TimeoutSeconds, len(cfg.Clients))
	}
}

func TestLoadCompareConfig_ParamShapesRoundTrip(t *testing.T) {
	path := writeCompareFixture(t, `
name: "shapes"
calls:
  m_object:
    - params:
        - key: "value"
          nested:
            n: 1
  m_null:
    - params: [null]
  m_bool:
    - params: [true, false]
  m_number:
    - params: [1, 2.5]
`)

	cfg, err := LoadCompareConfig(path)
	if err != nil {
		t.Fatalf("LoadCompareConfig: %v", err)
	}

	if p := cfg.CustomParameters["m_null_variant1"]; len(p) != 1 || p[0] != nil {
		t.Errorf("null round-trip failed: %v", p)
	}
	if p := cfg.CustomParameters["m_bool_variant1"]; len(p) != 2 || p[0] != true || p[1] != false {
		t.Errorf("bool round-trip failed: %v", p)
	}
	// yaml.v3 decodes numbers as int / float64.
	if p := cfg.CustomParameters["m_number_variant1"]; len(p) != 2 {
		t.Errorf("number round-trip length = %d, want 2 (%v)", len(p), p)
	}
	obj, ok := cfg.CustomParameters["m_object_variant1"][0].(map[string]interface{})
	if !ok {
		t.Fatalf("object round-trip failed, got %T", cfg.CustomParameters["m_object_variant1"][0])
	}
	if obj["key"] != "value" {
		t.Errorf("object.key round-trip failed: %v", obj["key"])
	}
	nested, ok := obj["nested"].(map[string]interface{})
	if !ok {
		t.Fatalf("nested object should be map, got %T", obj["nested"])
	}
	if _, ok := nested["n"]; !ok {
		t.Errorf("nested.n missing: %v", nested)
	}
}

func TestLoadCompareConfig_Errors(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "missing name",
			content: `
calls:
  eth_blockNumber:
    - params: []
`,
			want: "'name' is required",
		},
		{
			name: "empty calls",
			content: `
name: "x"
calls: {}
`,
			want: "'calls' must contain at least one method",
		},
		{
			name: "empty per-method list",
			content: `
name: "x"
calls:
  eth_blockNumber: []
`,
			want: `method "eth_blockNumber" has no calls`,
		},
		{
			name: "missing params",
			content: `
name: "x"
calls:
  eth_blockNumber:
    - id: "no-params"
`,
			want: "is missing required 'params'",
		},
		{
			name: "duplicate id",
			content: `
name: "x"
calls:
  eth_getBalance:
    - id: "same"
      params: []
    - id: "same"
      params: []
`,
			want: `id "same" is duplicated`,
		},
		{
			name: "invalid id chars",
			content: `
name: "x"
calls:
  eth_getBalance:
    - id: "bad id!"
      params: []
`,
			want: "must match [a-zA-Z0-9_-]+",
		},
		{
			name: "reserved id",
			content: `
name: "x"
calls:
  eth_getBalance:
    - id: "variant1"
      params: []
`,
			want: "collides with the reserved 'variant<N>' form",
		},
		{
			name: "duplicate method",
			content: `
name: "x"
calls:
  eth_getBalance:
    - params: []
  eth_getBalance:
    - params: []
`,
			want: `method "eth_getBalance" is listed more than once`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeCompareFixture(t, tc.content)
			_, err := LoadCompareConfig(path)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}
