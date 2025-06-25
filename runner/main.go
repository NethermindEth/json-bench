package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/jsonrpc-bench/runner/comparator"
	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/generator"
)

func main() {
	// Parse command-line flags
	configPath := flag.String("config", "", "Path to YAML configuration file")
	outputDir := flag.String("output", "results", "Directory to store results")
	compareResponses := flag.Bool("compare", false, "Compare JSON-RPC responses across clients")
	validateSchema := flag.Bool("validate", true, "Validate responses against OpenRPC schema")
	concurrency := flag.Int("concurrency", 5, "Number of concurrent requests for comparison")
	timeout := flag.Int("timeout", 30, "Request timeout in seconds for comparison")
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

	// Run response comparison if enabled
	if *compareResponses {
		fmt.Println("\nStarting JSON-RPC response comparison...")
		if err := runComparison(cfg, *outputDir, *validateSchema, *concurrency, *timeout); err != nil {
			log.Printf("Response comparison warning: %v", err)
		} else {
			fmt.Println("Response comparison completed successfully!")
		}
	}
}

// runComparison runs a comparison of JSON-RPC responses across all clients in the config
func runComparison(cfg *config.Config, outputDir string, validateSchema bool, concurrency, timeout int) error {
	// Create clients list for the comparator
	var clientsList []comparator.Client
	for _, client := range cfg.Clients {
		clientsList = append(clientsList, comparator.Client{
			Name: client.Name,
			URL:  client.URL,
		})
	}

	// Extract methods from endpoints
	methods := make([]string, 0, len(cfg.Endpoints))
	for _, endpoint := range cfg.Endpoints {
		methods = append(methods, endpoint.Method)
	}

	// Create comparison config
	compConfig := &comparator.ComparisonConfig{
		Name:                 "Benchmark Response Comparison",
		Description:          "Comparing JSON-RPC responses across clients from benchmark config",
		Clients:              clientsList,
		Methods:              methods,
		ValidateAgainstSchema: validateSchema,
		Concurrency:          concurrency,
		TimeoutSeconds:       timeout,
		OutputDir:            outputDir,
	}

	// Create comparator
	comp, err := comparator.NewComparator(compConfig)
	if err != nil {
		return fmt.Errorf("failed to create comparator: %w", err)
	}

	// Run comparison
	results, err := comp.Run()
	if err != nil {
		return fmt.Errorf("comparison failed: %w", err)
	}
	fmt.Printf("Completed comparison of %d methods\n", len(results))

	// Save results to JSON file
	jsonPath := filepath.Join(outputDir, "comparison-results.json")
	if err := comp.SaveResults(jsonPath); err != nil {
		return fmt.Errorf("failed to save comparison results: %w", err)
	}
	fmt.Printf("Comparison results saved to %s\n", jsonPath)

	// Generate HTML report
	htmlPath := filepath.Join(outputDir, "comparison-report.html")
	if err := comp.GenerateHTMLReport(htmlPath); err != nil {
		return fmt.Errorf("failed to generate comparison HTML report: %w", err)
	}
	fmt.Printf("Comparison HTML report generated at %s\n", htmlPath)

	return nil
}
