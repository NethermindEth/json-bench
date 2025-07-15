package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jsonrpc-bench/runner/comparator"
)

func main() {
	// Define command-line flags
	configName := flag.String("name", "JSON-RPC Comparison", "Name of the comparison run")
	configDesc := flag.String("desc", "Comparing JSON-RPC responses across Ethereum clients", "Description of the comparison run")
	methodsStr := flag.String("methods", "eth_blockNumber,eth_chainId,eth_gasPrice", "Comma-separated list of JSON-RPC methods to compare")
	outputPath := flag.String("output", "comparison-results", "Output directory for results")
	validateSchema := flag.Bool("validate", true, "Validate responses against OpenRPC schema")
	concurrency := flag.Int("concurrency", 5, "Number of concurrent requests")
	timeout := flag.Int("timeout", 30, "Request timeout in seconds")
	clientsStr := flag.String("clients", "", "Comma-separated list of client endpoints in format name:url")

	flag.Parse()

	// Parse methods
	methods := strings.Split(*methodsStr, ",")
	for i, method := range methods {
		methods[i] = strings.TrimSpace(method)
	}

	// Parse clients
	var clientsList []comparator.Client
	if *clientsStr == "" {
		log.Fatal("No clients specified. Use -clients flag to specify clients.")
	}

	clientPairs := strings.Split(*clientsStr, ",")
	for _, pair := range clientPairs {
		parts := strings.SplitN(pair, ":", 2)
		if len(parts) != 2 {
			log.Fatalf("Invalid client format: %s. Expected format: name:url", pair)
		}
		name := strings.TrimSpace(parts[0])
		url := strings.TrimSpace(parts[1])
		clientsList = append(clientsList, comparator.Client{
			Name: name,
			URL:  url,
		})
	}

	// Create comparison config
	config := &comparator.ComparisonConfig{
		Name:                  *configName,
		Description:           *configDesc,
		Clients:               clientsList,
		Methods:               methods,
		ValidateAgainstSchema: *validateSchema,
		Concurrency:           *concurrency,
		TimeoutSeconds:        *timeout,
		OutputDir:             *outputPath,
	}

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
	if err := os.MkdirAll(*outputPath, 0755); err != nil {
		log.Fatalf("Failed to create output directory: %v", err)
	}

	// Save results to JSON file
	jsonPath := filepath.Join(*outputPath, "comparison-results.json")
	if err := comp.SaveResults(jsonPath); err != nil {
		log.Fatalf("Failed to save results: %v", err)
	}
	fmt.Printf("Results saved to %s\n", jsonPath)

	// Generate HTML report
	htmlPath := filepath.Join(*outputPath, "comparison-report.html")
	if err := comp.GenerateHTMLReport(htmlPath); err != nil {
		log.Fatalf("Failed to generate HTML report: %v", err)
	}
	fmt.Printf("HTML report generated at %s\n", htmlPath)
}
