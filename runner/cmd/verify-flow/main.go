package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/generator"
	"github.com/jsonrpc-bench/runner/storage"
	"github.com/jsonrpc-bench/runner/types"
	"github.com/sirupsen/logrus"
)

func main() {
	// Step 1: Run K6 and get results
	fmt.Println("=== Step 1: Running K6 benchmark ===")
	results, err := generator.RunK6Benchmark("/Users/parithosh/dev/eth2/json-bench/results/k6-script.js", "/Users/parithosh/dev/eth2/json-bench/results")
	if err != nil {
		log.Printf("K6 execution warning: %v", err)
	}

	fmt.Printf("K6 Results - Client metrics count: %d\n", len(results.ClientMetrics))
	for client, metrics := range results.ClientMetrics {
		fmt.Printf("  - %s: %d requests, %.2f%% error rate\n", client, metrics.TotalRequests, metrics.ErrorRate)
	}

	// Step 2: Create a minimal config
	fmt.Println("\n=== Step 2: Creating minimal config ===")
	cfg := &config.Config{
		TestName: "verify-flow-test",
		Clients: []config.Client{
			{Name: "geth", URL: "http://localhost:8545"},
		},
	}

	// Step 3: Load storage config and save
	fmt.Println("\n=== Step 3: Saving to historic storage ===")
	logger := logrus.New()
	storageCfg, err := config.LoadStorageConfig("/Users/parithosh/dev/eth2/json-bench/config/storage-example.yaml", logger)
	if err != nil {
		log.Fatalf("Failed to load storage config: %v", err)
	}

	historicStorage, err := storage.NewHistoricStorage(storageCfg)
	if err != nil {
		log.Fatalf("Failed to create historic storage: %v", err)
	}

	// Check ClientMetrics before save
	fmt.Printf("\nBefore save - ClientMetrics count: %d\n", len(results.ClientMetrics))

	savedRun, err := historicStorage.SaveRun(results, cfg)
	if err != nil {
		log.Fatalf("Failed to save run: %v", err)
	}

	fmt.Printf("\nSaved run ID: %s\n", savedRun.ID)

	// Step 4: Verify what was saved
	fmt.Println("\n=== Step 4: Verifying saved data ===")

	// Parse the FullResults JSON
	var fullResults types.BenchmarkResult
	if err := json.Unmarshal(savedRun.FullResults, &fullResults); err != nil {
		log.Printf("Failed to unmarshal FullResults: %v", err)
	} else {
		fmt.Printf("Unmarshaled FullResults - ClientMetrics count: %d\n", len(fullResults.ClientMetrics))
		for client := range fullResults.ClientMetrics {
			fmt.Printf("  - %s\n", client)
		}
	}

	// Step 5: Retrieve from database
	fmt.Println("\n=== Step 5: Retrieving from database ===")
	retrievedRun, err := historicStorage.GetHistoricRun(nil, savedRun.ID)
	if err != nil {
		log.Printf("Failed to retrieve run: %v", err)
	} else {
		fmt.Printf("Retrieved run ID: %s\n", retrievedRun.ID)

		// Check FullResults
		var retrievedResults types.BenchmarkResult
		if err := json.Unmarshal(retrievedRun.FullResults, &retrievedResults); err != nil {
			log.Printf("Failed to unmarshal retrieved FullResults: %v", err)
		} else {
			fmt.Printf("Retrieved FullResults - ClientMetrics count: %d\n", len(retrievedResults.ClientMetrics))
			for client, metrics := range retrievedResults.ClientMetrics {
				fmt.Printf("  - %s: %d requests\n", client, metrics.TotalRequests)
			}
		}
	}

	// Save debug output
	debugData := map[string]interface{}{
		"original_results": results,
		"saved_run":        savedRun,
		"retrieved_run":    retrievedRun,
		"timestamp":        time.Now(),
	}

	jsonData, _ := json.MarshalIndent(debugData, "", "  ")
	if err := os.WriteFile("verify_flow_debug.json", jsonData, 0644); err != nil {
		log.Printf("Failed to write debug output: %v", err)
	} else {
		fmt.Println("\nDebug output saved to: verify_flow_debug.json")
	}
}
