package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/types"
)

func TestParseThreshold(t *testing.T) {
	summary := types.MetricSummary{
		Min: 1, Avg: 10, P50: 8, P75: 12, P90: 20, P95: 25, P99: 40, P999: 60, Max: 90,
		Count: 100, ErrorRate: 2,
	}

	cases := []struct {
		expression string
		pass       bool
		actual     float64
	}{
		// The only form the committed configs use.
		{expression: "p(99)<600000", pass: true, actual: 40},
		{expression: "p(99)<10", pass: false, actual: 40},
		{expression: "p(95) < 30", pass: true, actual: 25},
		{expression: "p(99.9)<=60", pass: true, actual: 60},
		{expression: "avg<10", pass: false, actual: 10},
		{expression: "avg<=10", pass: true, actual: 10},
		{expression: "med<9", pass: true, actual: 8},
		{expression: "max>50", pass: true, actual: 90},
		{expression: "count>=100", pass: true, actual: 100},
		// k6 writes error-rate thresholds as a fraction, while the summary
		// carries a percentage.
		{expression: "rate<0.01", pass: false, actual: 0.02},
		{expression: "rate<0.05", pass: true, actual: 0.02},
	}

	for _, tc := range cases {
		t.Run(tc.expression, func(t *testing.T) {
			threshold, err := ParseThreshold("eth_call", tc.expression)
			require.NoError(t, err)

			pass, actual, err := threshold.Evaluate(summary)
			require.NoError(t, err)
			assert.Equal(t, tc.pass, pass)
			assert.InDelta(t, tc.actual, actual, 0.0001)
		})
	}
}

func TestParseThresholdRejectsUnusableExpressions(t *testing.T) {
	for _, expression := range []string{
		"",
		"p(99)",
		"p(99) ~ 5",
		"nonsense<5",
		"p(42)<5",
		"p(99)<abc",
	} {
		t.Run(expression, func(t *testing.T) {
			_, err := ParseThreshold("eth_call", expression)
			assert.Error(t, err)
		})
	}
}

func TestEvaluateThresholds(t *testing.T) {
	clients := map[string]*types.ClientMetrics{
		"fast": {Methods: map[string]types.MetricSummary{"eth_call": {P99: 20}}},
		"slow": {Methods: map[string]types.MetricSummary{"eth_call": {P99: 900}}},
	}

	threshold, err := ParseThreshold("eth_call", "p(99)<100")
	require.NoError(t, err)

	breaches, err := EvaluateThresholds([]Threshold{threshold}, clients)
	require.NoError(t, err)

	require.Len(t, breaches, 1)
	assert.Equal(t, "slow", breaches[0].Client)
	assert.EqualValues(t, 900, breaches[0].Actual)
	assert.Contains(t, breaches[0].String(), "p(99)<100")
}

// A threshold on a method that never ran must not pass by default: an empty run
// satisfying every condition is exactly how a broken run looks successful.
func TestEvaluateThresholdsFailsWhenTheTargetSawNoTraffic(t *testing.T) {
	clients := map[string]*types.ClientMetrics{
		"c": {Methods: map[string]types.MetricSummary{"eth_getLogs": {P99: 5}}},
	}

	threshold, err := ParseThreshold("eth_call", "p(99)<100")
	require.NoError(t, err)

	breaches, err := EvaluateThresholds([]Threshold{threshold}, clients)
	require.NoError(t, err)

	require.Len(t, breaches, 1)
	assert.Contains(t, breaches[0].Expression, "no requests recorded")
}

// Every threshold expression in the committed profiles must still parse.
func TestCommittedThresholdFormParses(t *testing.T) {
	for _, expression := range []string{
		"p(99)<600000",
		"p(99)<1815389",
		"p(99)<50000",
		"p(99)<5769690",
	} {
		_, err := ParseThreshold("eth_call", expression)
		assert.NoError(t, err, expression)
	}
}
