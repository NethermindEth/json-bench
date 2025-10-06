package config

import (
	"fmt"
	"time"

	"github.com/jsonrpc-bench/runner/types"
)

// Config represents the benchmark configuration
type Config struct {
	TestName          string                `yaml:"test_name"`
	Description       string                `yaml:"description"`
	ClientRefs        []string              `yaml:"clients"`
	Duration          string                `yaml:"duration"`
	RPS               int                   `yaml:"rps"`
	VUs               int                   `yaml:"vus"`
	Calls             []*Call               `yaml:"calls"`
	CallsFile         string                `yaml:"calls_file"` // Optional: use file containing RPC calls instead of generating them
	ValidateResponses bool                  `yaml:"validate_responses"`
	ResolvedClients   []*types.ClientConfig `yaml:"-"`
	Outputs           *Outputs              `yaml:"-"`
}

// validateConfig performs validation on the loaded configuration
func validateConfig(cfg *Config) error {
	if cfg.TestName == "" {
		return fmt.Errorf("test_name is required")
	}

	if len(cfg.ClientRefs) == 0 {
		return fmt.Errorf("at least one client is required")
	}

	if len(cfg.Calls) == 0 && cfg.CallsFile == "" {
		return fmt.Errorf("at least one call is required")
	}

	if cfg.CallsFile == "" {
		for _, call := range cfg.Calls {
			if call.Name == "" {
				return fmt.Errorf("call name is required")
			}

			if call.File == "" {
				if call.Method == "" || call.Params == nil {
					return fmt.Errorf("call must have a method and params defined if no file is provided")
				}
			}
		}
	}

	// Validate duration
	if cfg.Duration == "" {
		return fmt.Errorf("duration is required")
	}
	_, err := time.ParseDuration(cfg.Duration)
	if err != nil {
		return fmt.Errorf("invalid duration format: %w", err)
	}

	if cfg.VUs <= 0 {
		return fmt.Errorf("vus must be greater than 0")
	}

	if cfg.RPS <= 0 {
		return fmt.Errorf("rps must be greater than 0")
	}

	return nil
}
