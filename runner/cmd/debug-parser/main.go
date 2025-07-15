package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/jsonrpc-bench/runner/generator"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: debug-parser <results-dir>")
		os.Exit(1)
	}

	resultsDir := os.Args[1]

	fmt.Printf("Parsing K6 results from: %s\n", resultsDir)

	clientMetrics, err := generator.ParseK6Results(resultsDir)
	if err != nil {
		log.Fatalf("Failed to parse K6 results: %v", err)
	}

	fmt.Printf("\nFound %d clients with metrics\n", len(clientMetrics))
	fmt.Println("=" + string(make([]byte, 50)) + "=")

	for clientName, metrics := range clientMetrics {
		fmt.Printf("\nClient: %s\n", clientName)
		fmt.Printf("  Total Requests: %d\n", metrics.TotalRequests)
		fmt.Printf("  Total Errors: %d\n", metrics.TotalErrors)
		successRate := 100.0 - metrics.ErrorRate
		fmt.Printf("  Success Rate: %.2f%%\n", successRate)
		fmt.Printf("  Error Rate: %.2f%%\n", metrics.ErrorRate)

		if metrics.Latency.Count > 0 {
			fmt.Printf("  Latency:\n")
			fmt.Printf("    Avg: %.2fms\n", metrics.Latency.Avg)
			fmt.Printf("    P95: %.2fms\n", metrics.Latency.P95)
			fmt.Printf("    P99: %.2fms\n", metrics.Latency.P99)
		}

		fmt.Printf("  Methods: %d\n", len(metrics.Methods))
		for methodName, method := range metrics.Methods {
			fmt.Printf("    %s: %d calls, %.2f%% success\n",
				methodName, method.Count, method.SuccessRate)
		}
	}

	// Output as JSON for inspection
	jsonData, _ := json.MarshalIndent(clientMetrics, "", "  ")
	outputPath := "parsed_metrics.json"
	if err := os.WriteFile(outputPath, jsonData, 0644); err != nil {
		log.Printf("Failed to write output: %v", err)
	} else {
		fmt.Printf("\nParsed metrics saved to: %s\n", outputPath)
	}
}
