package exporter

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/jsonrpc-bench/runner/types"
)

// DataExporter handles exporting benchmark results to various formats
type DataExporter struct {
	outputDir string
}

// NewDataExporter creates a new data exporter
func NewDataExporter(outputDir string) *DataExporter {
	return &DataExporter{
		outputDir: outputDir,
	}
}

// ExportAll exports data to all supported formats
func (de *DataExporter) ExportAll(result *types.BenchmarkResult) error {
	// Create export directory
	exportDir := filepath.Join(de.outputDir, "exports")
	if err := os.MkdirAll(exportDir, 0755); err != nil {
		return fmt.Errorf("failed to create export directory: %w", err)
	}

	// Export to different formats
	if err := de.ExportJSON(result, filepath.Join(exportDir, "results.json")); err != nil {
		return fmt.Errorf("failed to export JSON: %w", err)
	}

	if err := de.ExportMethodMetricsCSV(result, filepath.Join(exportDir, "method_metrics.csv")); err != nil {
		return fmt.Errorf("failed to export method metrics CSV: %w", err)
	}

	if err := de.ExportClientComparisonCSV(result, filepath.Join(exportDir, "client_comparison.csv")); err != nil {
		return fmt.Errorf("failed to export client comparison CSV: %w", err)
	}

	if err := de.ExportTimeSeriesCSV(result, filepath.Join(exportDir, "time_series.csv")); err != nil {
		return fmt.Errorf("failed to export time series CSV: %w", err)
	}

	if err := de.ExportSystemMetricsCSV(result, filepath.Join(exportDir, "system_metrics.csv")); err != nil {
		return fmt.Errorf("failed to export system metrics CSV: %w", err)
	}

	return nil
}

// ExportJSON exports the complete result to JSON
func (de *DataExporter) ExportJSON(result *types.BenchmarkResult, outputPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")

	return encoder.Encode(result)
}

// ExportMethodMetricsCSV exports detailed method metrics to CSV
func (de *DataExporter) ExportMethodMetricsCSV(result *types.BenchmarkResult, outputPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	header := []string{
		"Client", "Method", "Count", "Success Rate (%)",
		"Min (ms)", "P50 (ms)", "P75 (ms)", "P90 (ms)", "P95 (ms)", "P99 (ms)", "P99.9 (ms)", "Max (ms)",
		"Avg (ms)", "Std Dev", "Variance", "CV (%)", "IQR", "MAD",
		"Throughput (req/s)", "Error Count", "Timeout Rate (%)", "Connection Errors",
	}

	if err := writer.Write(header); err != nil {
		return err
	}

	// Write data rows
	for clientName, client := range result.ClientMetrics {
		for methodName, metrics := range client.Methods {
			row := []string{
				clientName,
				methodName,
				strconv.FormatInt(metrics.Count, 10),
				fmt.Sprintf("%.2f", metrics.SuccessRate),
				fmt.Sprintf("%.2f", metrics.Min),
				fmt.Sprintf("%.2f", metrics.P50),
				fmt.Sprintf("%.2f", metrics.P75),
				fmt.Sprintf("%.2f", metrics.P90),
				fmt.Sprintf("%.2f", metrics.P95),
				fmt.Sprintf("%.2f", metrics.P99),
				fmt.Sprintf("%.2f", metrics.P999),
				fmt.Sprintf("%.2f", metrics.Max),
				fmt.Sprintf("%.2f", metrics.Avg),
				fmt.Sprintf("%.2f", metrics.StdDev),
				fmt.Sprintf("%.2f", metrics.Variance),
				fmt.Sprintf("%.2f", metrics.CoeffVar),
				fmt.Sprintf("%.2f", metrics.IQR),
				fmt.Sprintf("%.2f", metrics.MAD),
				fmt.Sprintf("%.2f", metrics.Throughput),
				strconv.FormatInt(int64(metrics.Count)-int64(metrics.SuccessRate*float64(metrics.Count)/100), 10),
				fmt.Sprintf("%.2f", metrics.TimeoutRate),
				strconv.FormatInt(metrics.ConnectionErrors, 10),
			}

			if err := writer.Write(row); err != nil {
				return err
			}
		}
	}

	return nil
}

// ExportClientComparisonCSV exports client-level comparison data
func (de *DataExporter) ExportClientComparisonCSV(result *types.BenchmarkResult, outputPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	header := []string{
		"Client", "Total Requests", "Total Errors", "Error Rate (%)",
		"Avg Latency (ms)", "P95 Latency (ms)", "Performance Score",
		"Active Connections", "Connection Reuse (%)", "DNS Resolution (ms)", "TLS Handshake (ms)",
	}

	if err := writer.Write(header); err != nil {
		return err
	}

	// Write data rows
	for clientName, client := range result.ClientMetrics {
		score := float64(0)
		if result.PerformanceScore != nil {
			score = result.PerformanceScore[clientName]
		}

		row := []string{
			clientName,
			strconv.FormatInt(client.TotalRequests, 10),
			strconv.FormatInt(client.TotalErrors, 10),
			fmt.Sprintf("%.2f", client.ErrorRate),
			fmt.Sprintf("%.2f", client.Latency.Avg),
			fmt.Sprintf("%.2f", client.Latency.P95),
			fmt.Sprintf("%.1f", score),
			strconv.FormatInt(client.ConnectionMetrics.ActiveConnections, 10),
			fmt.Sprintf("%.1f", client.ConnectionMetrics.ConnectionReuse),
			fmt.Sprintf("%.2f", client.ConnectionMetrics.DNSResolutionTime),
			fmt.Sprintf("%.2f", client.ConnectionMetrics.TLSHandshakeTime),
		}

		if err := writer.Write(row); err != nil {
			return err
		}
	}

	return nil
}

// ExportTimeSeriesCSV exports time series data for analysis
func (de *DataExporter) ExportTimeSeriesCSV(result *types.BenchmarkResult, outputPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	header := []string{"Timestamp", "Client", "Metric", "Value", "Count", "Error Count"}
	if err := writer.Write(header); err != nil {
		return err
	}

	// Export time series data for each client
	for clientName, client := range result.ClientMetrics {
		if client.TimeSeries == nil {
			continue
		}

		for metricName, points := range client.TimeSeries {
			for _, point := range points {
				timestamp := time.Unix(0, point.Timestamp*int64(time.Millisecond))
				row := []string{
					timestamp.Format(time.RFC3339),
					clientName,
					metricName,
					fmt.Sprintf("%.2f", point.Value),
					strconv.FormatInt(point.Count, 10),
					strconv.FormatInt(point.ErrorCount, 10),
				}

				if err := writer.Write(row); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// ExportSystemMetricsCSV exports system resource metrics
func (de *DataExporter) ExportSystemMetricsCSV(result *types.BenchmarkResult, outputPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	header := []string{
		"Timestamp", "Client", "CPU Usage (%)", "Memory (MB)", "Memory (%)",
		"Network Sent (bytes)", "Network Recv (bytes)",
		"Disk Read (bytes)", "Disk Write (bytes)",
		"Open Connections", "Goroutines",
	}

	if err := writer.Write(header); err != nil {
		return err
	}

	// Export system metrics for each client
	for clientName, client := range result.ClientMetrics {
		for i, metrics := range client.SystemMetrics {
			// Estimate timestamp based on index
			timestamp := time.Now().Add(time.Duration(i) * time.Second)

			row := []string{
				timestamp.Format(time.RFC3339),
				clientName,
				fmt.Sprintf("%.2f", metrics.CPUUsage),
				fmt.Sprintf("%.2f", metrics.MemoryUsage),
				fmt.Sprintf("%.2f", metrics.MemoryPercent),
				strconv.FormatInt(metrics.NetworkBytesSent, 10),
				strconv.FormatInt(metrics.NetworkBytesRecv, 10),
				strconv.FormatInt(metrics.DiskIORead, 10),
				strconv.FormatInt(metrics.DiskIOWrite, 10),
				strconv.FormatInt(metrics.OpenConnections, 10),
				strconv.Itoa(metrics.GoroutineCount),
			}

			if err := writer.Write(row); err != nil {
				return err
			}
		}
	}

	return nil
}

// ExportMarkdownSummary exports a markdown summary of the results
func (de *DataExporter) ExportMarkdownSummary(result *types.BenchmarkResult, outputPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Write markdown summary
	fmt.Fprintf(file, "# Benchmark Results Summary\n\n")
	fmt.Fprintf(file, "**Test Name:** %s\n", result.Config.(map[string]interface{})["test_name"])
	fmt.Fprintf(file, "**Duration:** %s\n", result.Duration)
	fmt.Fprintf(file, "**Started:** %s\n\n", result.StartTime)

	// Environment
	fmt.Fprintf(file, "## Environment\n\n")
	fmt.Fprintf(file, "- **OS:** %s %s\n", result.Environment.OS, result.Environment.Architecture)
	fmt.Fprintf(file, "- **CPU:** %s (%d cores)\n", result.Environment.CPUModel, result.Environment.CPUCores)
	fmt.Fprintf(file, "- **Memory:** %.1f GB\n", result.Environment.TotalMemoryGB)
	fmt.Fprintf(file, "- **Go Version:** %s\n\n", result.Environment.GoVersion)

	// Performance Summary
	if result.Comparison != nil {
		fmt.Fprintf(file, "## Performance Summary\n\n")
		fmt.Fprintf(file, "**Winner:** %s (Score: %.1f)\n\n", result.Comparison.Winner, result.Comparison.WinnerScore)

		fmt.Fprintf(file, "### Client Rankings\n\n")
		fmt.Fprintf(file, "| Client | Score | Relative Performance |\n")
		fmt.Fprintf(file, "|--------|-------|---------------------|\n")

		for client, score := range result.PerformanceScore {
			relPerf := result.Comparison.RelativePerf[client]
			fmt.Fprintf(file, "| %s | %.1f | %.1f%% |\n", client, score, relPerf)
		}
		fmt.Fprintf(file, "\n")
	}

	// Key Metrics
	fmt.Fprintf(file, "## Key Metrics\n\n")
	fmt.Fprintf(file, "| Client | Requests | Success Rate | P95 Latency | Throughput |\n")
	fmt.Fprintf(file, "|--------|----------|--------------|-------------|------------|\n")

	for name, client := range result.ClientMetrics {
		successRate := 100.0 - client.ErrorRate
		avgThroughput := float64(0)
		methodCount := 0

		for _, method := range client.Methods {
			avgThroughput += method.Throughput
			methodCount++
		}
		if methodCount > 0 {
			avgThroughput /= float64(methodCount)
		}

		fmt.Fprintf(file, "| %s | %d | %.1f%% | %.1fms | %.1f req/s |\n",
			name, client.TotalRequests, successRate, client.Latency.P95, avgThroughput)
	}

	// Recommendations
	if len(result.Recommendations) > 0 {
		fmt.Fprintf(file, "\n## Recommendations\n\n")
		for _, rec := range result.Recommendations {
			fmt.Fprintf(file, "- %s\n", rec)
		}
	}

	return nil
}
