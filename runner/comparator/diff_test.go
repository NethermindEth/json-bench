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
	ctx := newDiffContext("", nil)

	// Test that 0x and 0x0000...0000 are considered equal
	diffs, err := deepCompare(ctx, "result", "0x", "0x0000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatalf("deepCompare returned error: %v", err)
	}
	if len(diffs) != 0 {
		t.Errorf("Expected no differences, got %d differences", len(diffs))
	}

	// Test the reverse order
	diffs, err = deepCompare(ctx, "result", "0x0000000000000000000000000000000000000000000000000000000000000000", "0x")
	if err != nil {
		t.Fatalf("deepCompare returned error: %v", err)
	}
	if len(diffs) != 0 {
		t.Errorf("Expected no differences, got %d differences", len(diffs))
	}

	// Test that other hex values are still considered different
	diffs, err = deepCompare(ctx, "result", "0x1", "0x0000000000000000000000000000000000000000000000000000000000000000")
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
	diffs, err := compareJSONRPCResponses(nil, resp1, resp2)
	if err != nil {
		t.Fatalf("compareJSONRPCResponses returned error: %v", err)
	}

	// Check that there are no differences in the result
	if _, ok := diffs["result_differences"]; ok {
		t.Errorf("Expected no result differences, but found some")
	}
}

func resultResp(result interface{}) map[string]interface{} {
	return map[string]interface{}{"jsonrpc": "2.0", "id": 1, "result": result}
}

func errorResp(code int, message string) map[string]interface{} {
	return map[string]interface{}{"jsonrpc": "2.0", "id": 1, "error": map[string]interface{}{
		"code": float64(code), "message": message,
	}}
}

func hasResultDiff(diffs map[string]interface{}) bool {
	_, ok := diffs["result_differences"]
	return ok
}

func TestNumericTolerance(t *testing.T) {
	rules := []ComparisonRule{{Method: "eth_estimateGas", Path: "result", Kind: RuleNumericTolerance, Abs: 32, Rel: 0.01}}
	ctx := newDiffContext("eth_estimateGas", rules)

	tests := []struct {
		name     string
		v1, v2   string
		wantDiff bool
	}{
		{"within abs", "0x571e", "0x5720", false},                     // diff 2 <= 32
		{"at abs boundary", "0x1000", "0x1020", false},                // diff 32 <= 32
		{"just past abs but within rel", "0x10000", "0x10021", false}, // diff 33, rel ~0.0005
		{"beyond both", "0x1000", "0x2000", true},                     // diff 4096, rel ~0.5
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			diffs, err := deepCompare(ctx, "result", tc.v1, tc.v2)
			if err != nil {
				t.Fatalf("deepCompare error: %v", err)
			}
			if got := len(diffs) > 0; got != tc.wantDiff {
				t.Errorf("diff=%v, want %v (diffs=%v)", got, tc.wantDiff, diffs)
			}
		})
	}
}

func TestDefaultEstimateGasTolerance(t *testing.T) {
	// No configured rules: eth_estimateGas gets the built-in 10% tolerance.
	ctx := newDiffContext("eth_estimateGas", nil)

	// 5% apart -> equal.
	diffs, _ := deepCompare(ctx, "result", "0x64", "0x69") // 100 vs 105
	if len(diffs) != 0 {
		t.Errorf("expected 5%% drift within default tolerance, got %v", diffs)
	}
	// 20% apart -> reported.
	diffs, _ = deepCompare(ctx, "result", "0x64", "0x78") // 100 vs 120
	if len(diffs) == 0 {
		t.Error("expected 20% drift to exceed default tolerance")
	}

	// A different method gets no implicit tolerance.
	other := newDiffContext("eth_getBalance", nil)
	diffs, _ = deepCompare(other, "result", "0x64", "0x69")
	if len(diffs) == 0 {
		t.Error("expected non-estimateGas method to report any difference")
	}
}

func TestIgnorePaths(t *testing.T) {
	rules := []ComparisonRule{
		{Path: "result.totalDifficulty", Kind: RuleIgnore},
		{Path: "result.transactions[*].v", Kind: RuleIgnore},
	}
	ctx := newDiffContext("eth_getBlockByNumber", rules)

	r1 := resultResp(map[string]interface{}{
		"number":          "0x1",
		"totalDifficulty": nil,
		"transactions":    []interface{}{map[string]interface{}{"v": "0x1b"}},
	})
	r2 := resultResp(map[string]interface{}{
		"number":          "0x1",
		"totalDifficulty": "0xabc",
		"transactions":    []interface{}{map[string]interface{}{"v": "0x25"}},
	})

	diffs, err := compareJSONRPCResponses(ctx, r1, r2)
	if err != nil {
		t.Fatalf("compare error: %v", err)
	}
	if hasResultDiff(diffs) {
		t.Errorf("ignored paths should collapse to no differences, got %v", diffs)
	}

	// Without the ignore rules the same responses differ.
	plain := newDiffContext("eth_getBlockByNumber", nil)
	diffs, _ = compareJSONRPCResponses(plain, r1, r2)
	if !hasResultDiff(diffs) {
		t.Error("expected differences without ignore rules")
	}
}

func TestErrorCodeOnly(t *testing.T) {
	ctx := newDiffContext("eth_call", []ComparisonRule{{Method: "eth_call", Kind: RuleErrorCodeOnly}})

	// Same code, different message -> equal.
	diffs, _ := compareJSONRPCResponses(ctx, errorResp(-32000, "insufficient funds"), errorResp(-32000, "insufficient sender balance"))
	if _, ok := diffs["error_differences"]; ok {
		t.Errorf("error_code_only should ignore message drift, got %v", diffs)
	}

	// Different code -> reported.
	diffs, _ = compareJSONRPCResponses(ctx, errorResp(-32000, "x"), errorResp(-32003, "x"))
	if _, ok := diffs["error_differences"]; !ok {
		t.Error("expected code mismatch to be reported")
	}
}

func TestErrorPresenceOnly(t *testing.T) {
	ctx := newDiffContext("eth_call", []ComparisonRule{{Method: "eth_call", Kind: RuleErrorPresenceOnly}})
	diffs, _ := compareJSONRPCResponses(ctx, errorResp(-32000, "a"), errorResp(-32602, "b"))
	if _, ok := diffs["error_differences"]; ok {
		t.Errorf("error_presence_only should treat any two errors as equal, got %v", diffs)
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		code int
		msg  string
		want string
	}{
		{-32601, "method not found", "namespace_disabled"},
		{-32002, "No state available", "no_state"},
		{-32602, "logs range too large", "range_cap"},
		{-32602, "invalid argument", ""},
		{-32000, "execution reverted", ""},
	}
	for _, tc := range tests {
		got := classifyError(errorResp(tc.code, tc.msg))
		if got != tc.want {
			t.Errorf("classifyError(%d,%q)=%q want %q", tc.code, tc.msg, got, tc.want)
		}
	}
	if got := classifyError(resultResp("0x1")); got != "" {
		t.Errorf("classifyError on result response = %q, want empty", got)
	}
}
