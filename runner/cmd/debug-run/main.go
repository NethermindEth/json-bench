package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/jsonrpc-bench/runner/generator"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: debug-run <k6-script> <output-dir>")
		os.Exit(1)
	}

	scriptPath := os.Args[1]
	outputDir := os.Args[2]

	fmt.Printf("Running K6 benchmark...\n")
	fmt.Printf("Script: %s\n", scriptPath)
	fmt.Printf("Output: %s\n", outputDir)

	results, err := generator.RunK6Benchmark(scriptPath, outputDir)
	if err != nil {
		log.Printf("Benchmark execution warning: %v", err)
	}

	fmt.Printf("\nBenchmark Results:\n")
	fmt.Printf("Duration: %s\n", results.Duration)
	fmt.Printf("Start Time: %s\n", results.StartTime)
	fmt.Printf("End Time: %s\n", results.EndTime)
	fmt.Printf("Client Metrics Count: %d\n", len(results.ClientMetrics))

	if len(results.ClientMetrics) == 0 {
		fmt.Println("\n⚠️  No client metrics found!")
		fmt.Println("Checking if summary exists...")

		summaryPath := outputDir + "/summary.json"
		if _, err := os.Stat(summaryPath); err != nil {
			fmt.Printf("Summary file not found: %s\n", summaryPath)
		} else {
			fmt.Println("Summary file exists")

			// Try to parse metrics manually
			clientMetrics, err := generator.ParseK6Results(outputDir)
			if err != nil {
				fmt.Printf("Failed to parse K6 results: %v\n", err)
			} else {
				fmt.Printf("Parsed client metrics count: %d\n", len(clientMetrics))
				for client, metrics := range clientMetrics {
					fmt.Printf("  - %s: %d requests\n", client, metrics.TotalRequests)
				}
			}
		}
	} else {
		fmt.Println("\nClient Metrics:")
		for client, metrics := range results.ClientMetrics {
			fmt.Printf("  - %s: %d requests, %.2f%% error rate\n",
				client, metrics.TotalRequests, metrics.ErrorRate)
		}
	}

	// Save results for inspection
	jsonData, _ := json.MarshalIndent(results, "", "  ")
	debugPath := "debug_results.json"
	if err := os.WriteFile(debugPath, jsonData, 0644); err != nil {
		log.Printf("Failed to write debug output: %v", err)
	} else {
		fmt.Printf("\nDebug results saved to: %s\n", debugPath)
	}
}
