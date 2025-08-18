package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jsonrpc-bench/runner/types"
	"gopkg.in/yaml.v3"
)

// Endpoint represents a JSON-RPC method to benchmark
type Endpoint struct {
	Name       string        `yaml:"name,omitempty"` // Optional: custom name for this endpoint
	Method     string        `yaml:"method"`
	Params     []interface{} `yaml:"params"`
	Frequency  string        `yaml:"frequency"`
	File       string        `yaml:"file,omitempty"`       // Optional: file containing RPC calls
	FileType   string        `yaml:"file_type,omitempty"`  // Type of file: "json" or "jsonl"
	Thresholds []string      `yaml:"thresholds,omitempty"` // Optional: request duration thresholds for this endpoint in the format of "p(95) < X". See https://k6.io/docs/using-k6/thresholds/
}

// GetFrequency returns the frequency of an endpoint as a percentage
func (e *Endpoint) GetFrequency() float64 {
	freq := strings.TrimSuffix(e.Frequency, "%")
	var percentage float64
	if _, err := fmt.Sscanf(freq, "%f", &percentage); err != nil {
		return -1
	}
	return percentage
}

// Config represents the benchmark configuration
type Config struct {
	TestName          string                `yaml:"test_name"`
	Description       string                `yaml:"description"`
	ClientRefs        []string              `yaml:"clients"`
	Duration          string                `yaml:"duration"`
	RPS               int                   `yaml:"rps"`
	Endpoints         []Endpoint            `yaml:"endpoints"`
	ValidateResponses bool                  `yaml:"validate_responses"`
	ResolvedClients   []*types.ClientConfig `yaml:"-"`
	Outputs           *Outputs              `yaml:"-"`
}

// LoadFromFile loads a benchmark configuration from a YAML file
func LoadFromFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Expand endpoints that reference files
	expandedEndpoints, err := ExpandEndpointsWithFiles(cfg.Endpoints)
	if err != nil {
		return nil, fmt.Errorf("failed to expand file-based endpoints: %w", err)
	}
	cfg.Endpoints = expandedEndpoints

	if err := validateConfig(&cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, nil
}

// validateConfig performs validation on the loaded configuration
func validateConfig(cfg *Config) error {
	if cfg.TestName == "" {
		return fmt.Errorf("test_name is required")
	}

	if len(cfg.ClientRefs) == 0 {
		return fmt.Errorf("at least one client is required")
	}

	if len(cfg.Endpoints) == 0 {
		return fmt.Errorf("at least one endpoint is required")
	}
	// Validate duration
	if cfg.Duration == "" {
		return fmt.Errorf("duration is required")
	}
	_, err := time.ParseDuration(cfg.Duration)
	if err != nil {
		return fmt.Errorf("invalid duration format: %w", err)
	}

	// Validate that frequency percentages add up to 100%
	totalFreq := 0
	for _, endpoint := range cfg.Endpoints {
		freq := strings.TrimSuffix(endpoint.Frequency, "%")
		var percentage int
		if _, err := fmt.Sscanf(freq, "%d", &percentage); err != nil {
			return fmt.Errorf("invalid frequency format for method %s: %s", endpoint.Method, endpoint.Frequency)
		}
		totalFreq += percentage
	}

	if totalFreq != 100 {
		return fmt.Errorf("endpoint frequencies must add up to 100%%, got %d%%", totalFreq)
	}

	return nil
}
