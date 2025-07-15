package config

import (
	"fmt"
	"log"
)

// ExampleConfigLoader demonstrates how to use the ConfigLoader
func ExampleConfigLoader() {
	// Step 1: Create a client registry and load client configurations
	registry := NewClientRegistry()

	// Load clients from a YAML file
	err := registry.LoadFromFile("clients.yaml")
	if err != nil {
		log.Fatalf("Failed to load clients: %v", err)
	}

	// Step 2: Create a config loader with the registry
	loader := NewConfigLoader(registry)

	// Step 3: Load a test configuration
	config, err := loader.LoadTestConfig("test_config.yaml")
	if err != nil {
		log.Fatalf("Failed to load test config: %v", err)
	}

	// The config now has resolved clients
	fmt.Printf("Test: %s\n", config.TestName)
	fmt.Printf("Description: %s\n", config.Description)
	fmt.Printf("Duration: %s\n", config.Duration)
	fmt.Printf("RPS: %d\n", config.RPS)

	fmt.Printf("\nClients:\n")
	for _, client := range config.ResolvedClients {
		fmt.Printf("  - %s: %s\n", client.Name, client.URL)
	}

	fmt.Printf("\nEndpoints:\n")
	for _, endpoint := range config.Endpoints {
		fmt.Printf("  - %s (%s)\n", endpoint.Method, endpoint.Frequency)
	}
}

// ExampleBackwardCompatibility shows how to load old-style configs
func ExampleBackwardCompatibility() {
	// Create a client registry (can be empty for old-style configs)
	registry := NewClientRegistry()
	loader := NewConfigLoader(registry)

	// This method automatically detects the config style
	config, err := loader.LoadWithBackwardCompatibility("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// The old-style embedded clients are automatically loaded into the registry
	// and the config is converted to the new format
	fmt.Printf("Loaded %d clients\n", len(config.ResolvedClients))
}
