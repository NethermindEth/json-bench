package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/generator"
)

func main() {
	// Parse command-line flags
	configPath := flag.String("config", "", "Path to YAML configuration file")
	outputDir := flag.String("output", "results", "Directory to store results")
	flag.Parse()

	if *configPath == "" {
		log.Fatal("Please provide a configuration file path using -config flag")
	}

	// Load configuration
	cfg, err := config.LoadFromFile(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		log.Fatalf("Failed to create output directory: %v", err)
	}

	// Generate k6 script
	scriptPath := filepath.Join(*outputDir, "k6-script.js")
	if err := generator.GenerateK6Script(cfg, scriptPath); err != nil {
		log.Fatalf("Failed to generate k6 script: %v", err)
	}
	fmt.Printf("Generated k6 script at: %s\n", scriptPath)

	// Run k6 benchmark
	fmt.Println("Running benchmark...")
	results, err := generator.RunK6Benchmark(scriptPath, *outputDir)
	if err != nil {
		// Log the error but continue to generate the report
		log.Printf("Benchmark execution warning: %v", err)
	}

	// Generate HTML report
	reportPath := filepath.Join(*outputDir, "report.html")
	if err := generator.GenerateHTMLReport(cfg, results, reportPath); err != nil {
		log.Fatalf("Failed to generate HTML report: %v", err)
	}
	fmt.Printf("Generated HTML report at: %s\n", reportPath)

	fmt.Println("Benchmark completed successfully!")
}
