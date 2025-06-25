package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/jsonrpc-bench/runner/comparator"
)

func main() {
	// Define command-line flags
	configPath := flag.String("config", "", "Path to YAML configuration file")
	flag.Parse()

	if *configPath == "" {
		log.Fatal("Please provide a configuration file path using -config flag")
	}

	// Load configuration
	fmt.Printf("Loading configuration from %s...\n", *configPath)
	config, err := comparator.LoadConfigFromYAML(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	fmt.Printf("Loaded configuration with %d methods and %d clients\n", len(config.Methods), len(config.Clients))

	// Create comparator
	comp, err := comparator.NewComparator(config)
	if err != nil {
		log.Fatalf("Failed to create comparator: %v", err)
	}

	// Run comparison
	fmt.Println("Starting JSON-RPC response comparison...")
	startTime := time.Now()
	results, err := comp.Run()
	if err != nil {
		log.Fatalf("Comparison failed: %v", err)
	}
	duration := time.Since(startTime)
	fmt.Printf("Comparison completed in %v with %d methods compared\n", duration, len(results))

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(config.OutputDir, 0755); err != nil {
		log.Fatalf("Failed to create output directory: %v", err)
	}

	// Save results to JSON file
	jsonPath := filepath.Join(config.OutputDir, "comparison-results.json")
	if err := comp.SaveResults(jsonPath); err != nil {
		log.Fatalf("Failed to save results: %v", err)
	}
	fmt.Printf("Results saved to %s\n", jsonPath)

	// Generate HTML report
	htmlPath := filepath.Join(config.OutputDir, "comparison-report.html")
	if err := comp.GenerateHTMLReport(htmlPath); err != nil {
		log.Fatalf("Failed to generate HTML report: %v", err)
	}
	fmt.Printf("HTML report generated at %s\n", htmlPath)
}
