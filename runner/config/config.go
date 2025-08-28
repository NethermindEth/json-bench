package config

import (
	"fmt"
	"os"
	"time"

	"github.com/jsonrpc-bench/runner/types"
	"gopkg.in/yaml.v3"
)

// Methods represents a JSON-RPC method to benchmark
type Method struct {
	Name       string        `yaml:"name"` // Custom name for this method
	Method     string        `yaml:"method"`
	Params     []interface{} `yaml:"params"`
	Weight     int           `yaml:"weight"`
	File       string        `yaml:"file,omitempty"`       // Optional: file containing RPC calls
	FileType   string        `yaml:"file_type,omitempty"`  // Type of file: "json" or "jsonl"
	Thresholds []string      `yaml:"thresholds,omitempty"` // Optional: request duration thresholds for this endpoint in the format of "p(95) < X". See https://k6.io/docs/using-k6/thresholds/
}

// Config represents the benchmark configuration
type Config struct {
	TestName          string                `yaml:"test_name"`
	Description       string                `yaml:"description"`
	ClientRefs        []string              `yaml:"clients"`
	Duration          string                `yaml:"duration"`
	RPS               int                   `yaml:"rps"`
	Methods           []Method              `yaml:"methods"`
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

	// Expand methods that reference files
	expandedMethods, err := ExpandMethodsWithFiles(cfg.Methods)
	if err != nil {
		return nil, fmt.Errorf("failed to expand file-based methods: %w", err)
	}
	cfg.Methods = expandedMethods

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

	if len(cfg.Methods) == 0 {
		return fmt.Errorf("at least one method is required")
	}
	// Validate duration
	if cfg.Duration == "" {
		return fmt.Errorf("duration is required")
	}
	_, err := time.ParseDuration(cfg.Duration)
	if err != nil {
		return fmt.Errorf("invalid duration format: %w", err)
	}

	return nil
}
