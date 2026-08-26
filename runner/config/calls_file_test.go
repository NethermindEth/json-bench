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

func TestMethodKeys(t *testing.T) {
	declared := &Config{Calls: []*Call{
		{Name: "blocks", Method: "eth_getBlockByNumber"},
		{Method: "eth_chainId"}, // no name: falls back to the method
	}}
	tag, ids := declared.MethodKeys()
	if tag != "req_name" {
		t.Errorf("tag = %q, want req_name for a declared-calls run", tag)
	}
	if len(ids) != 2 || ids[0] != "blocks" || ids[1] != "eth_chainId" {
		t.Errorf("identifiers = %v, want [blocks eth_chainId]", ids)
	}

	fromFile := &Config{
		Calls:            []*Call{{Name: "multimethod", Method: "eth_call"}},
		CallsFile:        "requests.csv",
		CallsFileMethods: []string{"eth_call", "eth_getLogs"},
	}
	tag, ids = fromFile.MethodKeys()
	if tag != "rpc_method" {
		t.Errorf("tag = %q, want rpc_method for a calls-file run", tag)
	}
	if len(ids) != 2 || ids[0] != "eth_call" || ids[1] != "eth_getLogs" {
		t.Errorf("identifiers = %v, want the calls-file methods, not the declared name", ids)
	}
}
