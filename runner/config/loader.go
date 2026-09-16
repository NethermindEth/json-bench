package config

import (
	"bytes"
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
	if err := UnmarshalStrict(data, &config); err != nil {
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
//
// The document is split rather than decoded into a struct mirroring Config:
// `clients` is separated from the rest and the rest is decoded by Config
// itself, so a field added to Config reaches this path too. A mirror struct
// silently dropped whatever nobody remembered to add to it, and dropping
// `warmup` or `batch_size` means running a different benchmark than the file
// asked for.
func (cl *ConfigLoader) loadOldStyleConfig(data []byte) (*Config, error) {
	clientsDoc, rest, err := splitEmbeddedClients(data)
	if err != nil {
		return nil, err
	}

	var embedded struct {
		Clients []types.ClientConfig `yaml:"clients"`
	}
	if err := UnmarshalStrict(clientsDoc, &embedded); err != nil {
		return nil, fmt.Errorf("failed to unmarshal the embedded clients: %w", err)
	}

	newConfig := &Config{}
	if err := UnmarshalStrict(rest, newConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal old-style config: %w", err)
	}
	newConfig.ClientRefs = make([]string, 0, len(embedded.Clients))
	newConfig.ResolvedClients = make([]*types.ClientConfig, 0, len(embedded.Clients))

	// If we have a client registry, register the embedded clients
	if cl.clientRegistry != nil {
		// Create a temporary clients config
		tempClientsConfig := types.ClientsConfig{
			Clients: embedded.Clients,
		}

		// Load the embedded clients into the registry
		if err := cl.clientRegistry.LoadFromConfig(tempClientsConfig); err != nil {
			return nil, fmt.Errorf("failed to load embedded clients into registry: %w", err)
		}
	}

	// Add client references and resolved clients
	for i := range embedded.Clients {
		client := &embedded.Clients[i]
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

// splitEmbeddedClients returns the document twice over: once with everything
// but the `clients` block blanked out, and once with only that block blanked.
// Both halves keep every other byte on the line the file had it on, so whichever
// decoder rejects a key reports the line the reader will find it on.
// Re-serialising the halves instead cost nothing but the line numbers, which
// then pointed into generated text nobody had written.
func splitEmbeddedClients(data []byte) (clientsDoc, rest []byte, err error) {
	from, to, ok := blockLinesOf(data, "clients")
	if !ok {
		return reserialiseAround(data, "clients")
	}

	lines := bytes.Split(data, []byte("\n"))
	blankedTo := func(want bool) []byte {
		kept := make([][]byte, len(lines))
		for i, line := range lines {
			if inside := i+1 >= from && i+1 <= to; inside == want {
				kept[i] = line
			}
		}
		return bytes.Join(kept, []byte("\n"))
	}
	return blankedTo(true), blankedTo(false), nil
}

// blockLinesOf finds the lines one top-level key and its value occupy: from the
// key's own line to the line before the next key. It reports false unless the
// document is a block mapping whose top-level keys each start a line of their
// own, which is the only shape a line range means anything for.
func blockLinesOf(data []byte, key string) (from, to int, ok bool) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) == 0 {
		return 0, 0, false
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode || root.Style != 0 {
		return 0, 0, false
	}

	keysOnLine := make(map[int]int, len(root.Content)/2)
	target := -1
	for i := 0; i < len(root.Content); i += 2 {
		keysOnLine[root.Content[i].Line]++
		if root.Content[i].Value == key {
			target = i
		}
	}
	if target < 0 {
		return 0, 0, false
	}

	from = root.Content[target].Line
	if keysOnLine[from] != 1 {
		return 0, 0, false
	}
	to = bytes.Count(data, []byte("\n")) + 1
	if next := target + 2; next < len(root.Content) {
		to = root.Content[next].Line - 1
	}
	return from, to, from <= to
}

// reserialiseAround splits a document whose lines cannot be divided — a
// top-level flow mapping has no line per key. The halves come back as generated
// YAML, so the line numbers in an error about them are lost; loading the file
// still beats refusing it over its layout.
func reserialiseAround(data []byte, key string) (withKey, without []byte, err error) {
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	withKey, err = yaml.Marshal(map[string]any{key: document[key]})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to re-read %s: %w", key, err)
	}

	delete(document, key)
	without, err = yaml.Marshal(document)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to re-read the config: %w", err)
	}
	return withKey, without, nil
}
