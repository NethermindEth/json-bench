package analyzer

import (
	"fmt"
	"math"
	"sort"

	"github.com/jsonrpc-bench/runner/types"
)

// PerformanceAnalyzer analyzes benchmark results and provides insights
type PerformanceAnalyzer struct {
	weights PerformanceWeights
}

// PerformanceWeights defines weights for different metrics in scoring
type PerformanceWeights struct {
	Latency    float64
	Throughput float64
	ErrorRate  float64
	Stability  float64
}

// NewPerformanceAnalyzer creates a new performance analyzer
func NewPerformanceAnalyzer() *PerformanceAnalyzer {
	return &PerformanceAnalyzer{
		weights: PerformanceWeights{
			Latency:    0.35,
			Throughput: 0.30,
			ErrorRate:  0.25,
			Stability:  0.10,
		},
	}
}

// AnalyzeResults performs comprehensive analysis on benchmark results
func (pa *PerformanceAnalyzer) AnalyzeResults(result *types.BenchmarkResult) {
	// Calculate performance scores
	result.PerformanceScore = pa.calculatePerformanceScores(result.ClientMetrics)

	// Perform comparison analysis
	result.Comparison = pa.compareClients(result.ClientMetrics, result.PerformanceScore)

	// Generate recommendations
	result.Recommendations = pa.generateRecommendations(result)
}

// calculatePerformanceScores calculates normalized performance scores for each client
func (pa *PerformanceAnalyzer) calculatePerformanceScores(clients map[string]*types.ClientMetrics) map[string]float64 {
	scores := make(map[string]float64)

	// Collect all metrics for normalization
	var allLatencies, allThroughputs, allErrorRates, allStabilities []float64
	clientNames := make([]string, 0, len(clients))

	for name, client := range clients {
		clientNames = append(clientNames, name)
		allLatencies = append(allLatencies, client.Latency.P95)

		// Calculate average throughput
		var totalThroughput float64
		methodCount := 0
		for _, method := range client.Methods {
			totalThroughput += method.Throughput
			methodCount++
		}
		avgThroughput := totalThroughput / float64(methodCount)
		allThroughputs = append(allThroughputs, avgThroughput)

		allErrorRates = append(allErrorRates, client.ErrorRate)

		// Calculate stability (coefficient of variation)
		stability := pa.calculateStability(client)
		allStabilities = append(allStabilities, stability)
	}

	// Normalize metrics (0-100 scale)
	latencyScores := pa.normalizeMetric(allLatencies, true)       // Lower is better
	throughputScores := pa.normalizeMetric(allThroughputs, false) // Higher is better
	errorScores := pa.normalizeMetric(allErrorRates, true)        // Lower is better
	stabilityScores := pa.normalizeMetric(allStabilities, true)   // Lower CV is better

	// Calculate weighted scores
	for i, name := range clientNames {
		score := pa.weights.Latency*latencyScores[i] +
			pa.weights.Throughput*throughputScores[i] +
			pa.weights.ErrorRate*errorScores[i] +
			pa.weights.Stability*stabilityScores[i]
		scores[name] = score
	}

	return scores
}

// normalizeMetric normalizes metrics to 0-100 scale
func (pa *PerformanceAnalyzer) normalizeMetric(values []float64, lowerIsBetter bool) []float64 {
	if len(values) == 0 {
		return []float64{}
	}

	min, max := pa.findMinMax(values)
	normalized := make([]float64, len(values))

	if max == min {
		// All values are the same
		for i := range normalized {
			normalized[i] = 50 // Middle score
		}
		return normalized
	}

	for i, val := range values {
		norm := (val - min) / (max - min)
		if lowerIsBetter {
			norm = 1 - norm // Invert so lower values get higher scores
		}
		normalized[i] = norm * 100
	}

	return normalized
}

// findMinMax finds minimum and maximum values in a slice
func (pa *PerformanceAnalyzer) findMinMax(values []float64) (float64, float64) {
	if len(values) == 0 {
		return 0, 0
	}

	min, max := values[0], values[0]
	for _, v := range values[1:] {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}

	return min, max
}

// calculateStability calculates the stability metric (average coefficient of variation)
func (pa *PerformanceAnalyzer) calculateStability(client *types.ClientMetrics) float64 {
	var totalCV float64
	count := 0

	for _, method := range client.Methods {
		if method.CoeffVar > 0 {
			totalCV += method.CoeffVar
			count++
		}
	}

	if count == 0 {
		return 0
	}

	return totalCV / float64(count)
}

// compareClients performs statistical comparison between clients
func (pa *PerformanceAnalyzer) compareClients(clients map[string]*types.ClientMetrics, scores map[string]float64) *types.ComparisonResult {
	if len(clients) < 2 {
		return nil
	}

	// Find winner based on scores
	var winner string
	var winnerScore float64
	for name, score := range scores {
		if score > winnerScore {
			winner = name
			winnerScore = score
		}
	}

	// Calculate relative performance
	relativePerf := make(map[string]float64)
	for name, score := range scores {
		relativePerf[name] = (score / winnerScore) * 100
	}

	// Find significant differences
	significantDiffs := pa.findSignificantDifferences(clients)

	// Calculate p-values for statistical significance (simplified)
	pValueMatrix := pa.calculatePValueMatrix(clients)

	return &types.ComparisonResult{
		Winner:           winner,
		WinnerScore:      winnerScore,
		RelativePerf:     relativePerf,
		SignificantDiffs: significantDiffs,
		PValueMatrix:     pValueMatrix,
	}
}

// findSignificantDifferences identifies significant performance differences
func (pa *PerformanceAnalyzer) findSignificantDifferences(clients map[string]*types.ClientMetrics) []string {
	var diffs []string

	// Compare latencies
	var latencies []struct {
		name    string
		latency float64
	}

	for name, client := range clients {
		latencies = append(latencies, struct {
			name    string
			latency float64
		}{name, client.Latency.P95})
	}

	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i].latency < latencies[j].latency
	})

	if len(latencies) >= 2 {
		best := latencies[0]
		worst := latencies[len(latencies)-1]

		if worst.latency > best.latency*1.5 { // 50% difference threshold
			diff := ((worst.latency - best.latency) / best.latency) * 100
			diffs = append(diffs, fmt.Sprintf("%s has %.1f%% higher P95 latency than %s",
				worst.name, diff, best.name))
		}
	}

	// Compare error rates
	for name, client := range clients {
		if client.ErrorRate > 5.0 {
			diffs = append(diffs, fmt.Sprintf("%s has high error rate: %.2f%%", name, client.ErrorRate))
		}
	}

	return diffs
}

// calculatePValueMatrix calculates simplified p-values for client comparisons
func (pa *PerformanceAnalyzer) calculatePValueMatrix(clients map[string]*types.ClientMetrics) map[string]map[string]float64 {
	matrix := make(map[string]map[string]float64)

	clientNames := make([]string, 0, len(clients))
	for name := range clients {
		clientNames = append(clientNames, name)
		matrix[name] = make(map[string]float64)
	}

	// Simplified p-value calculation based on latency differences
	for i, name1 := range clientNames {
		for j, name2 := range clientNames {
			if i == j {
				matrix[name1][name2] = 1.0
				continue
			}

			client1 := clients[name1]
			client2 := clients[name2]

			// Calculate simplified p-value based on latency difference
			diff := math.Abs(client1.Latency.P95 - client2.Latency.P95)
			avgLatency := (client1.Latency.P95 + client2.Latency.P95) / 2

			if avgLatency > 0 {
				relDiff := diff / avgLatency
				// Simplified: smaller differences = higher p-value
				pValue := math.Exp(-relDiff * 10)
				matrix[name1][name2] = pValue
			} else {
				matrix[name1][name2] = 1.0
			}
		}
	}

	return matrix
}

// generateRecommendations generates performance recommendations based on analysis
func (pa *PerformanceAnalyzer) generateRecommendations(result *types.BenchmarkResult) []string {
	var recommendations []string

	for name, client := range result.ClientMetrics {
		// High error rate recommendations
		if client.ErrorRate > 10 {
			recommendations = append(recommendations,
				fmt.Sprintf("🚨 %s: Critical error rate (%.1f%%). Check server health, connection limits, and timeout settings.",
					name, client.ErrorRate))
		} else if client.ErrorRate > 5 {
			recommendations = append(recommendations,
				fmt.Sprintf("⚠️  %s: Elevated error rate (%.1f%%). Consider increasing timeouts or connection pool size.",
					name, client.ErrorRate))
		}

		// High latency recommendations
		if client.Latency.P95 > 1000 {
			recommendations = append(recommendations,
				fmt.Sprintf("🐌 %s: High P95 latency (%.0fms). Investigate slow queries, database locks, or resource constraints.",
					name, client.Latency.P95))
		}

		// High variability
		var highVarMethods []string
		for method, metrics := range client.Methods {
			if metrics.CoeffVar > 100 {
				highVarMethods = append(highVarMethods, method)
			}
		}
		if len(highVarMethods) > 0 {
			recommendations = append(recommendations,
				fmt.Sprintf("📊 %s: High latency variability in methods: %v. This indicates inconsistent performance.",
					name, highVarMethods))
		}

	}

	// System-level recommendations
	if result.Environment.CPUCores < 4 {
		recommendations = append(recommendations,
			"💻 System: Limited CPU cores detected. Consider running benchmarks on a more powerful machine for accurate results.")
	}

	// General best practices
	if len(recommendations) == 0 {
		recommendations = append(recommendations,
			"✅ All clients performing well within acceptable parameters.",
			"💡 Consider increasing load to find performance limits.",
			"📈 Monitor performance over time to detect regressions.")
	}

	return recommendations
}

// DetectRegression compares current results with baseline
func (pa *PerformanceAnalyzer) DetectRegression(current, baseline *types.BenchmarkResult) []string {
	var regressions []string

	for name, currentClient := range current.ClientMetrics {
		if baselineClient, exists := baseline.ClientMetrics[name]; exists {
			// Check latency regression
			latencyIncrease := ((currentClient.Latency.P95 - baselineClient.Latency.P95) / baselineClient.Latency.P95) * 100
			if latencyIncrease > 10 {
				regressions = append(regressions,
					fmt.Sprintf("⚠️  %s: P95 latency increased by %.1f%% (%.1fms → %.1fms)",
						name, latencyIncrease, baselineClient.Latency.P95, currentClient.Latency.P95))
			}

			// Check error rate regression
			errorIncrease := currentClient.ErrorRate - baselineClient.ErrorRate
			if errorIncrease > 1 {
				regressions = append(regressions,
					fmt.Sprintf("🚨 %s: Error rate increased by %.1f%% (%.1f%% → %.1f%%)",
						name, errorIncrease, baselineClient.ErrorRate, currentClient.ErrorRate))
			}

			// Check throughput regression
			for method, currentMetrics := range currentClient.Methods {
				if baselineMetrics, exists := baselineClient.Methods[method]; exists {
					throughputDecrease := ((baselineMetrics.Throughput - currentMetrics.Throughput) / baselineMetrics.Throughput) * 100
					if throughputDecrease > 15 {
						regressions = append(regressions,
							fmt.Sprintf("📉 %s/%s: Throughput decreased by %.1f%%",
								name, method, throughputDecrease))
					}
				}
			}
		}
	}

	return regressions
}
