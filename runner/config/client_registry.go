package config

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/jsonrpc-bench/runner/types"
	"gopkg.in/yaml.v3"
)

// ClientRegistry manages client configurations and provides thread-safe access
type ClientRegistry struct {
	mu      sync.RWMutex
	clients map[string]*types.ClientConfig
}

// NewClientRegistry creates a new ClientRegistry instance
func NewClientRegistry() *ClientRegistry {
	return &ClientRegistry{
		clients: make(map[string]*types.ClientConfig),
	}
}

// LoadFromFile reads a YAML file and loads client configurations
func (cr *ClientRegistry) LoadFromFile(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read clients config file: %w", err)
	}

	// Substitute environment variables
	content := string(data)
	substituted, err := SubstituteEnvVars(content)
	if err != nil {
		return fmt.Errorf("failed to substitute environment variables: %w", err)
	}
	data = []byte(substituted)

	var config types.ClientsConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to unmarshal clients config: %w", err)
	}

	return cr.LoadFromConfig(config)
}

// LoadFromConfig loads client configurations from a ClientsConfig struct
func (cr *ClientRegistry) LoadFromConfig(config types.ClientsConfig) error {
	// Validate the configuration before loading
	if err := cr.validateConfig(config); err != nil {
		return fmt.Errorf("invalid clients configuration: %w", err)
	}

	cr.mu.Lock()
	defer cr.mu.Unlock()

	// Clear existing clients
	cr.clients = make(map[string]*types.ClientConfig, len(config.Clients))

	// Load all clients
	for i := range config.Clients {
		client := config.Clients[i]
		cr.clients[client.Name] = &client
	}

	return nil
}

// Get retrieves a client configuration by name
func (cr *ClientRegistry) Get(name string) (*types.ClientConfig, bool) {
	cr.mu.RLock()
	defer cr.mu.RUnlock()

	client, exists := cr.clients[name]
	if !exists {
		return nil, false
	}

	// Return a copy to prevent external modification
	clientCopy := *client
	return &clientCopy, true
}

// GetAll returns all client configurations
func (cr *ClientRegistry) GetAll() map[string]types.ClientConfig {
	cr.mu.RLock()
	defer cr.mu.RUnlock()

	// Create a copy of the map to prevent external modification
	result := make(map[string]types.ClientConfig, len(cr.clients))
	for name, client := range cr.clients {
		result[name] = *client
	}

	return result
}

// Validate checks the configuration for errors
func (cr *ClientRegistry) Validate(config types.ClientsConfig) error {
	return cr.validateConfig(config)
}

// validateConfig performs validation checks on the client configuration
func (cr *ClientRegistry) validateConfig(config types.ClientsConfig) error {
	if len(config.Clients) == 0 {
		return fmt.Errorf("no clients configured")
	}

	// Check for duplicate names
	names := make(map[string]struct{}, len(config.Clients))
	for i, client := range config.Clients {
		// Validate name
		if client.Name == "" {
			return fmt.Errorf("client at index %d has empty name", i)
		}

		// Check for dashes in client name
		if strings.Contains(client.Name, "-") {
			return fmt.Errorf("client name '%s' contains dashes, which are not allowed. Use underscores instead", client.Name)
		}

		// Check for duplicates
		if _, exists := names[client.Name]; exists {
			return fmt.Errorf("duplicate client name: %s", client.Name)
		}
		names[client.Name] = struct{}{}

		// Validate URL
		if client.URL == "" {
			return fmt.Errorf("client %s has empty URL", client.Name)
		}

		// Basic URL validation - check it starts with http:// or https://
		if len(client.URL) < 7 || (client.URL[:7] != "http://" && (len(client.URL) < 8 || client.URL[:8] != "https://")) {
			return fmt.Errorf("client %s has invalid URL: %s (must start with http:// or https://)", client.Name, client.URL)
		}

		// Validate auth configuration if present
		if client.Auth != nil {
			if err := validateAuthConfig(client.Auth); err != nil {
				return fmt.Errorf("client %s has invalid auth configuration: %w", client.Name, err)
			}
		}

		// Validate rate limit configuration if present
		if client.RateLimit != nil {
			if client.RateLimit.RequestsPerSecond <= 0 {
				return fmt.Errorf("client %s has invalid rate limit: requests_per_second must be positive", client.Name)
			}
			if client.RateLimit.Burst < 0 {
				return fmt.Errorf("client %s has invalid rate limit: burst cannot be negative", client.Name)
			}
		}
	}

	return nil
}

// validateAuthConfig validates authentication configuration
func validateAuthConfig(auth *types.AuthConfig) error {
	switch auth.Type {
	case "basic":
		if auth.Username == "" || auth.Password == "" {
			return fmt.Errorf("basic auth requires username and password")
		}
	case "bearer":
		if auth.Token == "" {
			return fmt.Errorf("bearer auth requires token")
		}
	case "api_key":
		if auth.APIKey == "" {
			return fmt.Errorf("api_key auth requires api_key")
		}
	case "":
		return fmt.Errorf("auth type cannot be empty")
	default:
		return fmt.Errorf("unsupported auth type: %s", auth.Type)
	}
	return nil
}
