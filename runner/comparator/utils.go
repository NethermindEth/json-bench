package comparator

import (
	"fmt"
	"path/filepath"

	"github.com/jsonrpc-bench/runner/config"
)

// RunComparison runs a comparison of JSON-RPC responses across all clients in the config
func RunComparison(cfg *config.Config, outputDir string, validateSchema bool, concurrency, timeout int) error {
	// Use resolved clients directly for the comparator
	clientsList := cfg.ResolvedClients

	// Extract methods from endpoints
	methods := make([]string, 0, len(cfg.Endpoints))
	for _, endpoint := range cfg.Endpoints {
		methods = append(methods, endpoint.Method)
	}

	// Create comparison config
	compConfig := &ComparisonConfig{
		Name:                  "Benchmark Response Comparison",
		Description:           "Comparing JSON-RPC responses across clients from benchmark config",
		Clients:               clientsList,
		Methods:               methods,
		ValidateAgainstSchema: validateSchema,
		Concurrency:           concurrency,
		TimeoutSeconds:        timeout,
		OutputDir:             outputDir,
	}

	// Create comparator
	comp, err := NewComparator(compConfig)
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
