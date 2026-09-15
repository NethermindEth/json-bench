package comparator

import (
	"encoding/json"
	"strings"
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

		// Pruned or unavailable state, reported under a reused -32000.
		{-32000, "missing trie node 0x3a4f (path ) state 0x3a4f is not available", "no_state"},
		{-32000, "Historical state for block 12345 is unavailable", "no_state"},
		{-32000, "pruned history unavailable", "pruned_history"},
		{4444, "Pruned history unavailable", "pruned_history"},
		{-32005, "query returned more than 10000 results", "range_cap"},
		{-32016, "eth_getLogs request was canceled due to enabled timeout", "internal_timeout"},

		// Genuine execution errors under the same -32000 must stay real
		// differences, or --fail-on-diff stops catching regressions.
		{-32000, "err: max fee per gas less than block base fee", ""},
		{-32000, "insufficient funds for transfer", ""},
		{-32000, "nonce too low", ""},
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

// TestStrictZeroHexEquivalence pins the difference strict mode restores: a
// client that answers with the bare "0x" and one that answers with an
// all-zero hex string are not saying the same thing, and no rule could switch
// the built-in equivalence off.
func TestStrictZeroHexEquivalence(t *testing.T) {
	pairs := []struct{ a, b string }{
		{"0x", "0x0"},
		{"0x", "0x00"},
		{"0x", "0x" + strings.Repeat("00", 32)},
		{"0x0", "0x"},
	}
	for _, p := range pairs {
		t.Run(p.a+" vs "+p.b, func(t *testing.T) {
			loose, err := deepCompare(newDiffContext("eth_call", nil), "result", p.a, p.b)
			if err != nil {
				t.Fatalf("deepCompare: %v", err)
			}
			if len(loose) != 0 {
				t.Errorf("the default must keep treating these as equal, got %v", loose)
			}

			strict, err := deepCompare(newDiffContextStrict("eth_call", nil, true), "result", p.a, p.b)
			if err != nil {
				t.Fatalf("deepCompare: %v", err)
			}
			if len(strict) != 1 || strict[0].Type != DiffTypeValueMismatch {
				t.Errorf("strict must report a value mismatch, got %v", strict)
			}
		})
	}
}

// TestStrictZeroHexInErrorData covers the same equivalence inside error.data,
// which reaches deepCompare through the error branch rather than the result
// branch and so has to be checked separately.
func TestStrictZeroHexInErrorData(t *testing.T) {
	revert := func(data string) map[string]interface{} {
		return map[string]interface{}{"jsonrpc": "2.0", "id": 1, "error": map[string]interface{}{
			"code": float64(3), "message": "execution reverted", "data": data,
		}}
	}

	loose, err := compareJSONRPCResponses(newDiffContext("eth_call", nil), revert("0x"), revert("0x00"))
	if err != nil {
		t.Fatalf("compareJSONRPCResponses: %v", err)
	}
	if len(loose) != 0 {
		t.Errorf("the default must keep treating these as equal, got %v", loose)
	}

	strict, err := compareJSONRPCResponses(newDiffContextStrict("eth_call", nil, true), revert("0x"), revert("0x00"))
	if err != nil {
		t.Fatalf("compareJSONRPCResponses: %v", err)
	}
	if _, ok := strict["error_differences"]; !ok {
		t.Errorf("strict must report an error difference, got %v", strict)
	}
}

// TestStrictKeepsExplicitRules proves strictness only removes implicit
// behaviour: a numeric_tolerance rule still applies, and an ignore rule still
// drops its path, under both modes.
func TestStrictKeepsExplicitRules(t *testing.T) {
	tolerance := []ComparisonRule{{Method: "eth_estimateGas", Path: "result", Kind: RuleNumericTolerance, Abs: 32}}
	diffs, err := deepCompare(newDiffContextStrict("eth_estimateGas", tolerance, true), "result", "0x5b9c", "0x5b9f")
	if err != nil {
		t.Fatalf("deepCompare: %v", err)
	}
	if len(diffs) != 0 {
		t.Errorf("an explicit tolerance must still apply under strict, got %v", diffs)
	}

	ignore := []ComparisonRule{{Method: "eth_call", Path: "result", Kind: RuleIgnore}}
	diffs, err = deepCompare(newDiffContextStrict("eth_call", ignore, true), "result", "0xaa", "0xbb")
	if err != nil {
		t.Fatalf("deepCompare: %v", err)
	}
	if len(diffs) != 0 {
		t.Errorf("an explicit ignore must still apply under strict, got %v", diffs)
	}
}

// TestExactIntegerComparison covers the second hidden equivalence: JSON
// numbers decoded into interface{} land on float64, whose 53-bit mantissa
// makes two different integers above 2**53 compare equal. Strict mode decodes
// with UseNumber, so the literal digits decide.
func TestExactIntegerComparison(t *testing.T) {
	const a, b = "12345678901234567890", "12345678901234567891"

	asFloat := func(s string) float64 {
		var f float64
		if err := json.Unmarshal([]byte(s), &f); err != nil {
			t.Fatalf("unmarshal %s: %v", s, err)
		}
		return f
	}
	if asFloat(a) != asFloat(b) {
		t.Fatalf("premise broken: %s and %s no longer collide as float64", a, b)
	}

	ctx := newDiffContext("eth_call", nil)
	diffs, err := deepCompare(ctx, "result", asFloat(a), asFloat(b))
	if err != nil {
		t.Fatalf("deepCompare: %v", err)
	}
	if len(diffs) != 0 {
		t.Errorf("float64 decoding is expected to hide this by default, got %v", diffs)
	}

	strictCtx := newDiffContextStrict("eth_call", nil, true)
	diffs, err = deepCompare(strictCtx, "result", json.Number(a), json.Number(b))
	if err != nil {
		t.Fatalf("deepCompare: %v", err)
	}
	if len(diffs) != 1 || diffs[0].Type != DiffTypeValueMismatch {
		t.Errorf("strict must report a value mismatch, got %v", diffs)
	}

	same, err := deepCompare(strictCtx, "result", json.Number(a), json.Number(a))
	if err != nil {
		t.Fatalf("deepCompare: %v", err)
	}
	if len(same) != 0 {
		t.Errorf("equal numbers must not differ, got %v", same)
	}
}

// TestNumericCode reads the JSON-RPC error code in both decoded shapes: an
// error classified as code 0 would silently leave every strict-mode run's
// env/real difference split wrong.
func TestNumericCode(t *testing.T) {
	for _, v := range []interface{}{float64(-32002), json.Number("-32002")} {
		got, ok := numericCode(v)
		if !ok || got != -32002 {
			t.Errorf("numericCode(%#v) = %d,%v; want -32002,true", v, got, ok)
		}
	}
	if _, ok := numericCode("-32002"); ok {
		t.Error("a string code must not be read as a number")
	}

	strictErr := map[string]interface{}{"jsonrpc": "2.0", "id": 1, "error": map[string]interface{}{
		"code": json.Number("-32002"), "message": "No state available",
	}}
	if got := classifyError(strictErr); got != ClassNoState {
		t.Errorf("classifyError with a json.Number code = %q, want %q", got, ClassNoState)
	}
}
