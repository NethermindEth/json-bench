package cmd

import "testing"

// TestStrictResponseComparisonFlag pins the flag's name and default. The name
// is a contract with the callers that compose the strict replay lane, and the
// default must stay off so existing invocations are unaffected.
func TestStrictResponseComparisonFlag(t *testing.T) {
	flag := compareCmd.Flags().Lookup("strict-response-comparison")
	if flag == nil {
		t.Fatal("compare has no --strict-response-comparison flag")
	}
	if flag.DefValue != "false" {
		t.Errorf("default = %q, want false", flag.DefValue)
	}
	if flag.Value.Type() != "bool" {
		t.Errorf("type = %q, want bool", flag.Value.Type())
	}
}
