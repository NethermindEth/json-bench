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
	specPath := flag.String("spec", "", "Path or URL to OpenRPC specification")
	variationsPath := flag.String("variations", "", "Path to parameter variations YAML file")
	clientsStr := flag.String("clients", "", "Comma-separated list of client endpoints in format name:url")
	outputDir := flag.String("output", "comparison-results", "Output directory for results")
	validateSchema := flag.Bool("validate", false, "Validate responses against OpenRPC schema")
	concurrency := flag.Int("concurrency", 5, "Number of concurrent requests")
	timeout := flag.Int("timeout", 30, "Request timeout in seconds")
	methodFilter := flag.String("filter", "", "Optional comma-separated list of methods to include (if empty, all methods are included)")
	verbose := flag.Bool("curl", false, "Log curl equivalent commands for each JSON-RPC request")
	flag.Parse()

	if *specPath == "" {
		log.Fatal("Please provide an OpenRPC specification path or URL using -spec flag")
	}

	if *clientsStr == "" {
		log.Fatal("No clients specified. Use -clients flag to specify clients.")
	}

	// Load methods from OpenRPC spec with optional parameter variations
	fmt.Printf("Loading methods from OpenRPC specification: %s...\n", *specPath)
	if *variationsPath != "" {
		fmt.Printf("Using parameter variations from: %s...\n", *variationsPath)
	}

	config, err := comparator.LoadMethodsFromOpenRPC(*specPath, *variationsPath)
	if err != nil {
		log.Fatalf("Failed to load OpenRPC specification: %v", err)
	}

	// Override config settings with command-line flags
	config.ValidateAgainstSchema = *validateSchema
	config.Concurrency = *concurrency
	config.TimeoutSeconds = *timeout
	config.OutputDir = *outputDir
	config.Verbose = *verbose

	// Parse clients
	var clientsList []comparator.Client
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
	config.Clients = clientsList

	// Apply method filter if specified
	if *methodFilter != "" {
		methodsToInclude := strings.Split(*methodFilter, ",")
		methodMap := make(map[string]bool)
		for _, m := range methodsToInclude {
			methodMap[strings.TrimSpace(m)] = true
		}

		filteredMethods := make([]string, 0)
		filteredParams := make(map[string][]interface{})
		for _, method := range config.Methods {
			// Check if the base method name is in the filter (for variants)
			baseName := method
			if idx := strings.Index(method, "_variant"); idx > 0 {
				baseName = method[:idx]
			}

			if methodMap[baseName] || methodMap[method] {
				filteredMethods = append(filteredMethods, method)
				if params, ok := config.CustomParameters[method]; ok {
					filteredParams[method] = params
				}
			}
		}
		config.Methods = filteredMethods
		config.CustomParameters = filteredParams
	}

	fmt.Printf("Loaded %d methods (including variations) from OpenRPC specification\n", len(config.Methods))

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
