package generator

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jsonrpc-bench/runner/types"
)

// K6MetricValue represents a k6 metric value
type K6MetricValue struct {
	Count  int64              `json:"count"`
	Rate   float64            `json:"rate"`
	Avg    float64            `json:"avg"`
	Min    float64            `json:"min"`
	Max    float64            `json:"max"`
	Med    float64            `json:"med"`
	P90    float64            `json:"p(90)"`
	P95    float64            `json:"p(95)"`
	P99    float64            `json:"p(99)"`
	StdDev float64            `json:"std_dev"`
	Values map[string]float64 `json:"values"`
}

// K6Summary represents the k6 summary output
type K6Summary struct {
	Metrics map[string]K6MetricValue `json:"metrics"`
	Root    map[string]interface{}   `json:"root_group"`
}

// ParseK6Results parses k6 results and extracts comprehensive metrics
func ParseK6Results(resultsDir string) (map[string]*types.ClientMetrics, error) {
	// Read the summary file
	summaryPath := filepath.Join(resultsDir, "summary.json")
	data, err := ioutil.ReadFile(summaryPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read summary file: %w", err)
	}

	var summary K6Summary
	if err := json.Unmarshal(data, &summary); err != nil {
		return nil, fmt.Errorf("failed to parse summary: %w", err)
	}

	// Extract client names from metrics
	clientNames := extractClientNames(summary.Metrics)
	clientMetrics := make(map[string]*types.ClientMetrics)

	// Initialize client metrics with enhanced fields
	for _, clientName := range clientNames {
		clientMetrics[clientName] = &types.ClientMetrics{
			Name:              clientName,
			Methods:           make(map[string]types.MetricSummary),
			ConnectionMetrics: types.ConnectionMetrics{},
			ErrorTypes:        make(map[string]int64),
			StatusCodes:       make(map[int]int64),
		}
	}

	// Process metrics
	for metricName, metricValue := range summary.Metrics {
		if strings.HasPrefix(metricName, "client_") && strings.Contains(metricName, "_method_") {
			// Parse client-specific method metrics
			parts := strings.Split(metricName, "_")
			if len(parts) >= 5 {
				clientName := parts[1]
				metricType := parts[2]

				if client, exists := clientMetrics[clientName]; exists {
					if metricType == "method" {
						if parts[3] == "calls" {
							methodName := strings.Join(parts[4:], "_")
							if _, exists := client.Methods[methodName]; !exists {
								client.Methods[methodName] = types.MetricSummary{}
							}
							method := client.Methods[methodName]
							method.Count = metricValue.Count
							client.Methods[methodName] = method
							// Add to total requests
							client.TotalRequests += metricValue.Count
						} else if parts[3] == "latency" {
							methodName := strings.Join(parts[4:], "_")
							if _, exists := client.Methods[methodName]; !exists {
								client.Methods[methodName] = types.MetricSummary{}
							}
							method := client.Methods[methodName]
							method.Min = metricValue.Min
							method.Max = metricValue.Max
							method.Avg = metricValue.Avg
							method.P50 = metricValue.Med
							method.P90 = metricValue.P90
							method.P95 = metricValue.P95
							method.P99 = metricValue.P99
							method.StdDev = calculateStdDev(metricValue)
							// Calculate additional metrics
							if method.Avg > 0 {
								method.CoeffVar = (method.StdDev / method.Avg) * 100
							}
							method.SuccessRate = 100.0 // Will be updated with error data
							client.Methods[methodName] = method

							// Validate percentiles
							warnings := ValidatePercentiles(metricValue, fmt.Sprintf("%s.%s", clientName, methodName))
							for _, warning := range warnings {
								log.Printf("WARNING: %s", warning)
							}
						} else if parts[3] == "errors" {
							methodName := strings.Join(parts[4:], "_")
							if _, exists := client.Methods[methodName]; !exists {
								client.Methods[methodName] = types.MetricSummary{}
							}
							method := client.Methods[methodName]
							errorCount := metricValue.Count
							if method.Count > 0 {
								method.ErrorRate = float64(errorCount) / float64(method.Count) * 100
								method.SuccessRate = 100.0 - method.ErrorRate
							}
							client.Methods[methodName] = method
							// Add to total errors
							client.TotalErrors += errorCount
						} else if parts[3] == "success" {
							// Handle success metrics if needed
							methodName := strings.Join(parts[4:], "_")
							if _, exists := client.Methods[methodName]; !exists {
								client.Methods[methodName] = types.MetricSummary{}
							}
							method := client.Methods[methodName]
							method.SuccessCount = metricValue.Count
							client.Methods[methodName] = method
						}
					}
				}
			}
		} else if metricName == "connection_reuse" {
			// Process connection reuse metrics
			connectionReuseCount := metricValue.Count
			for _, client := range clientMetrics {
				if client.TotalRequests > 0 {
					client.ConnectionMetrics.ConnectionReuse = (float64(connectionReuseCount) / float64(client.TotalRequests)) * 100
				}
			}
		} else if metricName == "dns_lookup_time" {
			// Process DNS lookup time
			for _, client := range clientMetrics {
				client.ConnectionMetrics.DNSResolutionTime = metricValue.Avg
			}
		} else if metricName == "tcp_handshake_time" {
			// Process TCP handshake time
			for _, client := range clientMetrics {
				client.ConnectionMetrics.TCPHandshakeTime = metricValue.Avg
			}
		}
	}

	// Calculate overall metrics for each client
	for _, client := range clientMetrics {
		if client.TotalRequests > 0 {
			client.ErrorRate = float64(client.TotalErrors) / float64(client.TotalRequests) * 100
		} else {
			// If TotalRequests wasn't set from rpc_calls metric, calculate from method counts
			var totalMethodCalls int64
			for _, method := range client.Methods {
				totalMethodCalls += method.Count
			}
			client.TotalRequests = totalMethodCalls
			if client.TotalRequests > 0 {
				client.ErrorRate = float64(client.TotalErrors) / float64(client.TotalRequests) * 100
			}
		}

		// Calculate overall latency from method latencies
		var totalLatency float64
		var totalCount int64
		var minLatency, maxLatency float64 = 999999, 0
		var p50Sum, p90Sum, p95Sum, p99Sum float64
		var methodCount int

		for methodName, method := range client.Methods {
			totalLatency += method.Avg * float64(method.Count)
			totalCount += method.Count
			p50Sum += method.P50
			p90Sum += method.P90
			p95Sum += method.P95
			p99Sum += method.P99
			methodCount++

			if method.Min < minLatency {
				minLatency = method.Min
			}
			if method.Max > maxLatency {
				maxLatency = method.Max
			}

			// Calculate throughput for each method
			if method.Avg > 0 {
				method.Throughput = 1000.0 / method.Avg // requests per second
				client.Methods[methodName] = method
			}
		}

		if totalCount > 0 {
			client.Latency.Avg = totalLatency / float64(totalCount)
			client.Latency.Min = minLatency
			client.Latency.Max = maxLatency
			if methodCount > 0 {
				client.Latency.P50 = p50Sum / float64(methodCount)
				client.Latency.P90 = p90Sum / float64(methodCount)
				client.Latency.P95 = p95Sum / float64(methodCount)
				client.Latency.P99 = p99Sum / float64(methodCount)
			}
			// Calculate overall throughput based on average latency
			if client.Latency.Avg > 0 {
				client.Latency.Throughput = 1000.0 / client.Latency.Avg // requests per second
			}
		}
	}

	// Log summary of p99 validation
	totalMethods := 0
	methodsWithP99 := 0
	methodsWithZeroP99 := 0

	for clientName, client := range clientMetrics {
		for methodName, method := range client.Methods {
			totalMethods++
			if method.P99 > 0 {
				methodsWithP99++
			} else if method.Count > 0 {
				// Only count as zero if there were actual calls
				methodsWithZeroP99++
				log.Printf("WARNING: Method %s.%s has zero p99 value (count: %d, avg: %.2f)",
					clientName, methodName, method.Count, method.Avg)
			}
		}
	}

	if totalMethods > 0 {
		p99Coverage := float64(methodsWithP99) / float64(totalMethods) * 100
		log.Printf("P99 Validation Summary: %d/%d methods have p99 values (%.1f%% coverage)",
			methodsWithP99, totalMethods, p99Coverage)
		if methodsWithZeroP99 > 0 {
			log.Printf("WARNING: %d methods with actual traffic have p99=0", methodsWithZeroP99)
		}
	}

	return clientMetrics, nil
}

// extractClientNames extracts unique client names from metrics
func extractClientNames(metrics map[string]K6MetricValue) []string {
	clientMap := make(map[string]bool)

	for metricName := range metrics {
		// Look for client_<name>_method_calls_ pattern
		if strings.HasPrefix(metricName, "client_") && strings.Contains(metricName, "_method_calls_") {
			parts := strings.Split(metricName, "_")
			if len(parts) >= 4 && parts[2] == "method" && parts[3] == "calls" {
				clientName := parts[1]
				// Skip generic metrics and validate it's an actual client name
				if clientName != "success" && clientName != "errors" && clientName != "" {
					// Double check this is a real client by looking for multiple method metrics
					hasMultipleMetrics := false
					for m := range metrics {
						if strings.HasPrefix(m, "client_"+clientName+"_method_latency_") {
							hasMultipleMetrics = true
							break
						}
					}
					if hasMultipleMetrics {
						clientMap[clientName] = true
					}
				}
			}
		}
	}

	// Convert map to sorted slice
	var clients []string
	for client := range clientMap {
		clients = append(clients, client)
	}
	sort.Strings(clients)

	return clients
}

// calculateStdDev calculates standard deviation from k6 metric
func calculateStdDev(metric K6MetricValue) float64 {
	if metric.Count <= 1 {
		return 0
	}

	// k6 doesn't directly provide std_dev, so we estimate it
	// Using the range method: stddev ≈ (max - min) / 4
	return (metric.Max - metric.Min) / 4
}

// ValidatePercentiles validates percentile values and returns warnings
func ValidatePercentiles(metric K6MetricValue, metricName string) []string {
	var warnings []string

	// Check if p99 is zero when we have valid data
	if metric.Count > 0 && metric.P99 == 0 {
		warnings = append(warnings, fmt.Sprintf("p99 value is 0 for metric %s (count: %d, avg: %.2f)", metricName, metric.Count, metric.Avg))
	}

	// Check if p95 is zero when we have valid data
	if metric.Count > 0 && metric.P95 == 0 {
		warnings = append(warnings, fmt.Sprintf("p95 value is 0 for metric %s (count: %d, avg: %.2f)", metricName, metric.Count, metric.Avg))
	}

	// Check if p90 is zero when we have valid data
	if metric.Count > 0 && metric.P90 == 0 {
		warnings = append(warnings, fmt.Sprintf("p90 value is 0 for metric %s (count: %d, avg: %.2f)", metricName, metric.Count, metric.Avg))
	}

	// Validate percentile ordering: p90 <= p95 <= p99
	if metric.P90 > 0 && metric.P95 > 0 && metric.P90 > metric.P95 {
		warnings = append(warnings, fmt.Sprintf("invalid percentile ordering for %s: p90 (%.2f) > p95 (%.2f)", metricName, metric.P90, metric.P95))
	}
	if metric.P95 > 0 && metric.P99 > 0 && metric.P95 > metric.P99 {
		warnings = append(warnings, fmt.Sprintf("invalid percentile ordering for %s: p95 (%.2f) > p99 (%.2f)", metricName, metric.P95, metric.P99))
	}

	// Check if percentiles exceed max value
	if metric.Max > 0 {
		if metric.P99 > metric.Max {
			warnings = append(warnings, fmt.Sprintf("p99 (%.2f) exceeds max (%.2f) for metric %s", metric.P99, metric.Max, metricName))
		}
		if metric.P95 > metric.Max {
			warnings = append(warnings, fmt.Sprintf("p95 (%.2f) exceeds max (%.2f) for metric %s", metric.P95, metric.Max, metricName))
		}
		if metric.P90 > metric.Max {
			warnings = append(warnings, fmt.Sprintf("p90 (%.2f) exceeds max (%.2f) for metric %s", metric.P90, metric.Max, metricName))
		}
	}

	// Check if percentiles are below min value
	if metric.Min > 0 {
		if metric.P90 > 0 && metric.P90 < metric.Min {
			warnings = append(warnings, fmt.Sprintf("p90 (%.2f) is below min (%.2f) for metric %s", metric.P90, metric.Min, metricName))
		}
		if metric.P95 > 0 && metric.P95 < metric.Min {
			warnings = append(warnings, fmt.Sprintf("p95 (%.2f) is below min (%.2f) for metric %s", metric.P95, metric.Min, metricName))
		}
		if metric.P99 > 0 && metric.P99 < metric.Min {
			warnings = append(warnings, fmt.Sprintf("p99 (%.2f) is below min (%.2f) for metric %s", metric.P99, metric.Min, metricName))
		}
	}

	return warnings
}

// CalculateDuration calculates the duration from start and end times
func CalculateDuration(startTime, endTime string) string {
	// This is a placeholder - in a real implementation, you'd parse the times
	// and calculate the actual duration
	return "1m0s"
}
