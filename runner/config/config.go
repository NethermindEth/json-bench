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
	Iterations        int                   `yaml:"iterations"`
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
		totalWeight := 0
		for _, call := range cfg.Calls {
			if call.Name == "" {
				return fmt.Errorf("call name is required")
			}

			if call.File == "" {
				if call.Method == "" || call.Params == nil {
					return fmt.Errorf("call must have a method and params defined if no file is provided")
				}
			}

			totalWeight += call.Weight
		}

		// k6_generator uses totalWeight as the denominator for request
		// distribution; if it's zero the generator emits an empty requests.csv
		// and every k6 iteration throws "No more requests found" silently. The
		// run then "succeeds" with zero HTTP requests. Fail loud here instead.
		// Note: YAML is parsed non-strictly so a misspelled key like
		// `frequency: "20/s"` is dropped without error — only `weight: N`
		// (integer) is honored on a Call.
		if totalWeight <= 0 {
			return fmt.Errorf("sum of call weights must be > 0; every call has weight=0 (note: only `weight: N` integer is honored on a call — `frequency:` keys are silently ignored by the YAML parser)")
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

	if cfg.Iterations > 0 && cfg.RPS > 0 {
		return fmt.Errorf("iterations and rps cannot be used together")
	}

	if cfg.Iterations <= 0 && cfg.RPS <= 0 {
		return fmt.Errorf("either iterations or rps must be greater than 0")
	}

	return nil
}
