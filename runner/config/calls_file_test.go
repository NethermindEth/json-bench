package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCallsFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "requests.csv")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write calls file: %v", err)
	}
	return path
}

func TestLoadCallsFileMethods(t *testing.T) {
	// Column 2 (name) is deliberately unrelated to any declared call, which is
	// exactly the case that used to collapse the per-method export.
	path := writeCallsFile(t, `1,multimethod,eth_call,"{""jsonrpc"":""2.0"",""method"":""eth_call"",""params"":[]}"
2,multimethod,eth_getLogs,"{""jsonrpc"":""2.0"",""method"":""eth_getLogs"",""params"":[]}"
3,something-else,eth_call,"{""jsonrpc"":""2.0"",""method"":""eth_call"",""params"":[]}"
4,multimethod,eth_getBalance,"{""jsonrpc"":""2.0"",""method"":""eth_getBalance"",""params"":[]}"
`)

	methods, err := LoadCallsFileMethods(path)
	if err != nil {
		t.Fatalf("LoadCallsFileMethods: %v", err)
	}
	want := []string{"eth_call", "eth_getLogs", "eth_getBalance"}
	if len(methods) != len(want) {
		t.Fatalf("methods = %v, want %v", methods, want)
	}
	for i := range want {
		if methods[i] != want[i] {
			t.Errorf("methods[%d] = %q, want %q (distinct, first-seen order)", i, methods[i], want[i])
		}
	}
}

func TestLoadCallsFileMethodsErrors(t *testing.T) {
	tests := []struct {
		name     string
		contents string
	}{
		{"empty file", ""},
		{"short row", "1,name,eth_call\n"},
		{"extra column", "1,name,eth_call,payload,surplus\n"},
		{"empty method", `1,name,,"{}"` + "\n"},
	}
	for _, tc := range tests {
		if _, err := LoadCallsFileMethods(writeCallsFile(t, tc.contents)); err == nil {
			t.Errorf("%s: expected a loud failure, got none", tc.name)
		}
	}

	if _, err := LoadCallsFileMethods(filepath.Join(t.TempDir(), "absent.csv")); err == nil {
		t.Error("expected a missing calls file to fail at load time")
	}
}

// A calls file can drive one method through many named parameter shapes; the
// names are the dimension the breakdown keys on, so both are read.
func TestLoadCallsFileLabels(t *testing.T) {
	path := writeCallsFile(t, `1,proof_head,eth_getProof,"{}"
2,proof_deep,eth_getProof,"{}"
3,proof_head,eth_getProof,"{}"
`)

	methods, names, err := LoadCallsFileLabels(path)
	if err != nil {
		t.Fatalf("LoadCallsFileLabels: %v", err)
	}
	if len(methods) != 1 || methods[0] != "eth_getProof" {
		t.Errorf("methods = %v, want one distinct method", methods)
	}
	if len(names) != 2 || names[0] != "proof_head" || names[1] != "proof_deep" {
		t.Errorf("names = %v, want [proof_head proof_deep] in first-seen order", names)
	}
}
