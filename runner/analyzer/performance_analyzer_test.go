package analyzer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/types"
)

func clientWith(name string, p95, errorRate float64) *types.ClientMetrics {
	return &types.ClientMetrics{
		Name:      name,
		ErrorRate: errorRate,
		Latency:   types.MetricSummary{P95: p95, Avg: p95 / 2, CoeffVar: 20},
		Methods: map[string]types.MetricSummary{
			"eth_call": {P95: p95, Avg: p95 / 2, Throughput: 100, CoeffVar: 20},
		},
	}
}

// The score is a min-max normalisation across the clients in one run. With a
// single client every normalised value collapses to the same point, so the
// score carries no information and must not be published as though it did.
func TestSingleClientGetsNoScoreOrComparison(t *testing.T) {
	result := &types.BenchmarkResult{
		ClientMetrics: map[string]*types.ClientMetrics{
			"only": clientWith("only", 50, 0),
		},
	}

	NewPerformanceAnalyzer().AnalyzeResults(result)

	assert.Nil(t, result.PerformanceScore)
	assert.Nil(t, result.Comparison)
}

func TestTwoClientsAreScoredAndRanked(t *testing.T) {
	result := &types.BenchmarkResult{
		ClientMetrics: map[string]*types.ClientMetrics{
			"fast": clientWith("fast", 20, 0),
			"slow": clientWith("slow", 200, 5),
		},
	}

	NewPerformanceAnalyzer().AnalyzeResults(result)

	require.NotNil(t, result.PerformanceScore)
	require.NotNil(t, result.Comparison)
	assert.Equal(t, "fast", result.Comparison.Winner)
	assert.Greater(t, result.PerformanceScore["fast"], result.PerformanceScore["slow"])
}

// The engine computes the pairwise test from the retained samples, which this
// package never sees. It must survive the heuristic scoring rather than being
// overwritten by it.
func TestAnalyzerKeepsTheEnginesPairwiseTest(t *testing.T) {
	methods := []types.MethodComparison{{
		Method: "eth_call", ClientA: "fast", ClientB: "slow",
		MedianAMs: 20, MedianBMs: 30, PValue: 0.0001, Faster: "fast",
	}}

	result := &types.BenchmarkResult{
		ClientMetrics: map[string]*types.ClientMetrics{
			"fast": clientWith("fast", 20, 0),
			"slow": clientWith("slow", 200, 5),
		},
		Comparison: &types.ComparisonResult{Methods: methods},
	}

	NewPerformanceAnalyzer().AnalyzeResults(result)

	require.NotNil(t, result.Comparison)
	assert.Equal(t, methods, result.Comparison.Methods)
	assert.NotEmpty(t, result.Comparison.Winner, "the heuristic fields are still filled in")
}
