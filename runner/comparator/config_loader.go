package comparator

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ComparisonConfigYAML represents the YAML configuration for comparison
type ComparisonConfigYAML struct {
	Name           string             `yaml:"name"`
	Description    string             `yaml:"description"`
	Clients        []ClientYAML       `yaml:"clients"`
	ValidateSchema bool               `yaml:"validate_schema"`
	Concurrency    int                `yaml:"concurrency"`
	TimeoutSeconds int                `yaml:"timeout_seconds"`
	OutputDir      string             `yaml:"output_dir"`
	Methods        []MethodConfigYAML `yaml:"methods"`
}

// ClientYAML represents a client configuration in YAML
type ClientYAML struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
}

// MethodConfigYAML represents a method configuration in YAML
type MethodConfigYAML struct {
	Name   string        `yaml:"name"`
	Params []interface{} `yaml:"params"`
}

// LoadConfigFromYAML loads a comparison configuration from a YAML file
func LoadConfigFromYAML(filePath string) (*ComparisonConfig, error) {
	// Read YAML file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse YAML
	var yamlConfig ComparisonConfigYAML
	if err := yaml.Unmarshal(data, &yamlConfig); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Convert to ComparisonConfig
	config := &ComparisonConfig{
		Name:                  yamlConfig.Name,
		Description:           yamlConfig.Description,
		ValidateAgainstSchema: yamlConfig.ValidateSchema,
		Concurrency:           yamlConfig.Concurrency,
		TimeoutSeconds:        yamlConfig.TimeoutSeconds,
		OutputDir:             yamlConfig.OutputDir,
	}

	// Set default values if not specified
	if config.Concurrency == 0 {
		config.Concurrency = 5
	}
	if config.TimeoutSeconds == 0 {
		config.TimeoutSeconds = 30
	}
	if config.OutputDir == "" {
		config.OutputDir = "comparison-results"
	}

	// Convert clients
	config.Clients = make([]Client, 0, len(yamlConfig.Clients))
	for _, client := range yamlConfig.Clients {
		config.Clients = append(config.Clients, Client{
			Name: client.Name,
			URL:  client.URL,
		})
	}

	// Extract methods
	config.Methods = make([]string, 0, len(yamlConfig.Methods))
	config.CustomParameters = make(map[string][]interface{})
	for _, method := range yamlConfig.Methods {
		config.Methods = append(config.Methods, method.Name)
		if len(method.Params) > 0 {
			config.CustomParameters[method.Name] = method.Params
		}
	}

	return config, nil
}
