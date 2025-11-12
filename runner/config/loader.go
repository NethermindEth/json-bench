package config

import (
	"fmt"
	"os"

	"github.com/jsonrpc-bench/runner/types"
	"gopkg.in/yaml.v3"
)

// ConfigLoader handles loading and resolving test configurations
type ConfigLoader struct {
	clientRegistry *ClientRegistry
}

// NewConfigLoader creates a new ConfigLoader instance
func NewConfigLoader(registry *ClientRegistry) *ConfigLoader {
	return &ConfigLoader{
		clientRegistry: registry,
	}
}

// LoadTestConfig loads a test configuration from a YAML file
func (cl *ConfigLoader) LoadTestConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read test config file: %w", err)
	}

	// Substitute environment variables
	content := string(data)
	substituted, err := SubstituteEnvVars(content)
	if err != nil {
		return nil, fmt.Errorf("failed to substitute environment variables: %w", err)
	}
	data = []byte(substituted)

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal test config: %w", err)
	}

	// Load calls that reference files
	for _, call := range config.Calls {
		if call.File != "" {
			if err := call.LoadFile(); err != nil {
				return nil, fmt.Errorf("failed to load file for call: %w", err)
			}
		}
	}

	// Validate the configuration
	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid test configuration: %w", err)
	}

	// Resolve client references
	if err := cl.ResolveClientReferences(&config); err != nil {
		return nil, fmt.Errorf("failed to resolve client references: %w", err)
	}

	return &config, nil
}

// ResolveClientReferences looks up client configurations from the registry
func (cl *ConfigLoader) ResolveClientReferences(config *Config) error {
	if cl.clientRegistry == nil {
		return fmt.Errorf("client registry is not initialized")
	}

	// Clear any existing resolved clients
	config.ResolvedClients = make([]*types.ClientConfig, 0, len(config.ClientRefs))

	// Resolve each client reference
	for _, clientRef := range config.ClientRefs {
		client, exists := cl.clientRegistry.Get(clientRef)
		if !exists {
			return fmt.Errorf("client not found in registry: %s", clientRef)
		}
		config.ResolvedClients = append(config.ResolvedClients, client)
	}

	if len(config.ResolvedClients) == 0 {
		return fmt.Errorf("no clients resolved from references")
	}

	return nil
}

// LoadWithBackwardCompatibility handles loading configurations that might contain
// old-style embedded client configurations
func (cl *ConfigLoader) LoadWithBackwardCompatibility(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Substitute environment variables
	content := string(data)
	substituted, err := SubstituteEnvVars(content)
	if err != nil {
		return nil, fmt.Errorf("failed to substitute environment variables: %w", err)
	}
	data = []byte(substituted)

	// First, try to unmarshal to check if it contains old-style client definitions
	var rawConfig map[string]interface{}
	if err := yaml.Unmarshal(data, &rawConfig); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Check if the config contains "clients" field with embedded definitions
	if clientsData, hasClients := rawConfig["clients"]; hasClients {
		// Check if it's an array of strings (new style) or array of objects (old style)
		if clientsArray, ok := clientsData.([]interface{}); ok && len(clientsArray) > 0 {
			// Check the first element to determine the format
			switch clientsArray[0].(type) {
			case string:
				// New style with client references, use normal loading
				return cl.LoadTestConfig(filename)
			case map[string]interface{}, map[interface{}]interface{}:
				// Old style with embedded client definitions
				return cl.loadOldStyleConfig(data)
			default:
				return nil, fmt.Errorf("unexpected client configuration format")
			}
		}
	}

	// No clients field or empty, try normal loading
	return cl.LoadTestConfig(filename)
}

// loadOldStyleConfig handles configurations with embedded client definitions
// Note: data should already have environment variables substituted
func (cl *ConfigLoader) loadOldStyleConfig(data []byte) (*Config, error) {
	// Define a structure that can hold both old and new style configurations
	type oldStyleConfig struct {
		TestName          string               `yaml:"test_name"`
		Description       string               `yaml:"description"`
		Clients           []types.ClientConfig `yaml:"clients"` // Old style: embedded clients
		Duration          string               `yaml:"duration"`
		RPS               int                  `yaml:"rps"`
		VUs               int                  `yaml:"vus"`
		Calls             []*Call              `yaml:"calls"`
		ValidateResponses bool                 `yaml:"validate_responses"`
	}

	var oldConfig oldStyleConfig
	if err := yaml.Unmarshal(data, &oldConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal old-style config: %w", err)
	}

	// Convert to new style config
	newConfig := &Config{
		TestName:          oldConfig.TestName,
		Description:       oldConfig.Description,
		Duration:          oldConfig.Duration,
		RPS:               oldConfig.RPS,
		VUs:               oldConfig.VUs,
		Calls:             oldConfig.Calls,
		ValidateResponses: oldConfig.ValidateResponses,
		ClientRefs:        make([]string, 0, len(oldConfig.Clients)),
		ResolvedClients:   make([]*types.ClientConfig, 0, len(oldConfig.Clients)),
	}

	// If we have a client registry, register the embedded clients
	if cl.clientRegistry != nil {
		// Create a temporary clients config
		tempClientsConfig := types.ClientsConfig{
			Clients: oldConfig.Clients,
		}

		// Load the embedded clients into the registry
		if err := cl.clientRegistry.LoadFromConfig(tempClientsConfig); err != nil {
			return nil, fmt.Errorf("failed to load embedded clients into registry: %w", err)
		}
	}

	// Add client references and resolved clients
	for i := range oldConfig.Clients {
		client := &oldConfig.Clients[i]
		newConfig.ClientRefs = append(newConfig.ClientRefs, client.Name)
		newConfig.ResolvedClients = append(newConfig.ResolvedClients, client)
	}

	// Load calls that reference files
	for _, call := range newConfig.Calls {
		if call.File != "" {
			if err := call.LoadFile(); err != nil {
				return nil, fmt.Errorf("failed to load file for call: %w", err)
			}
		}
	}

	// Validate the configuration
	if err := validateConfig(newConfig); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return newConfig, nil
}
