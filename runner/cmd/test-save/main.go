package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/generator"
	"github.com/jsonrpc-bench/runner/storage"
	"github.com/sirupsen/logrus"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Println("Usage: test-save <config.yaml> <k6-script> <output-dir>")
		os.Exit(1)
	}

	configPath := os.Args[1]
	scriptPath := os.Args[2]
	outputDir := os.Args[3]

	// Load config
	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize logger
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	// Run benchmark
	fmt.Println("Running K6 benchmark...")
	results, err := generator.RunK6Benchmark(scriptPath, outputDir)
	if err != nil {
		log.Printf("Benchmark warning: %v", err)
	}

	fmt.Printf("Client metrics count before save: %d\n", len(results.ClientMetrics))
	for client := range results.ClientMetrics {
		fmt.Printf("  - %s\n", client)
	}

	// Load storage config
	storageCfg, err := config.LoadStorageConfig("/Users/parithosh/dev/eth2/json-bench/config/storage-example.yaml", logger)
	if err != nil {
		log.Fatalf("Failed to load storage config: %v", err)
	}

	// Initialize storage
	historicStorage, err := storage.NewHistoricStorage(storageCfg)
	if err != nil {
		log.Fatalf("Failed to create historic storage: %v", err)
	}

	// Save the run
	fmt.Println("\nSaving to historic storage...")
	savedRun, err := historicStorage.SaveRun(results, cfg)
	if err != nil {
		log.Fatalf("Failed to save run: %v", err)
	}

	fmt.Printf("\nSaved run ID: %s\n", savedRun.ID)

	// Check what was saved
	var fullResults map[string]interface{}
	if err := json.Unmarshal(savedRun.FullResults, &fullResults); err != nil {
		log.Printf("Failed to parse saved full_results: %v", err)
	} else {
		if clientMetrics, ok := fullResults["ClientMetrics"]; ok {
			fmt.Printf("ClientMetrics in saved full_results: %v\n", clientMetrics != nil)
			if cm, ok := clientMetrics.(map[string]interface{}); ok {
				fmt.Printf("Number of clients in saved data: %d\n", len(cm))
				for client := range cm {
					fmt.Printf("  - %s\n", client)
				}
			}
		} else {
			fmt.Println("⚠️  ClientMetrics NOT found in saved full_results!")
		}
	}
}
