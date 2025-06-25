package comparator

import (
	"testing"
)

func TestIsZeroHex(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"0x", true},
		{"0x0", true},
		{"0x00", true},
		{"0x0000000000000000000000000000000000000000000000000000000000000000", true},
		{"0x1", false},
		{"0x01", false},
		{"0x0000000000000000000000000000000000000000000000000000000000000001", false},
		{"1x0", false},
		{"", false},
		{"0", false},
	}

	for _, test := range tests {
		result := isZeroHex(test.input)
		if result != test.expected {
			t.Errorf("isZeroHex(%q) = %v; want %v", test.input, result, test.expected)
		}
	}
}

func TestDeepCompareZeroHex(t *testing.T) {
	// Test that 0x and 0x0000...0000 are considered equal
	diffs, err := deepCompare("result", "0x", "0x0000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatalf("deepCompare returned error: %v", err)
	}
	if len(diffs) != 0 {
		t.Errorf("Expected no differences, got %d differences", len(diffs))
	}

	// Test the reverse order
	diffs, err = deepCompare("result", "0x0000000000000000000000000000000000000000000000000000000000000000", "0x")
	if err != nil {
		t.Fatalf("deepCompare returned error: %v", err)
	}
	if len(diffs) != 0 {
		t.Errorf("Expected no differences, got %d differences", len(diffs))
	}

	// Test that other hex values are still considered different
	diffs, err = deepCompare("result", "0x1", "0x0000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatalf("deepCompare returned error: %v", err)
	}
	if len(diffs) == 0 {
		t.Errorf("Expected differences, got none")
	}
}

func TestCompareJSONRPCResponsesZeroHex(t *testing.T) {
	// Create two responses with different zero hex representations
	resp1 := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"result":  "0x",
	}

	resp2 := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"result":  "0x0000000000000000000000000000000000000000000000000000000000000000",
	}

	// Compare the responses
	diffs, err := compareJSONRPCResponses(resp1, resp2)
	if err != nil {
		t.Fatalf("compareJSONRPCResponses returned error: %v", err)
	}

	// Check that there are no differences in the result
	if _, ok := diffs["result_differences"]; ok {
		t.Errorf("Expected no result differences, but found some")
	}
}
