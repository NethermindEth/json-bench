package config

import (
	"fmt"
	"time"

	"github.com/jsonrpc-bench/runner/types"
)

// Config represents the benchmark configuration
type Config struct {
	TestName        string                `yaml:"test_name"`
	Description     string                `yaml:"description"`
	ClientRefs      []string              `yaml:"clients"`
	Duration        string                `yaml:"duration"`
	RPS             int                   `yaml:"rps"`
	Iterations      int                   `yaml:"iterations"`
	VUs             int                   `yaml:"vus"`
	Seed            int64                 `yaml:"seed"` // Optional: fixes the request sequence so repeated runs replay identical requests
	Calls           []*Call               `yaml:"calls"`
	CallsFile       string                `yaml:"calls_file"` // Optional: use file containing RPC calls instead of generating them
	ResolvedClients []*types.ClientConfig `yaml:"-"`
	Outputs         *Outputs              `yaml:"-"`

	// CallsFileMethods holds the distinct RPC methods found in CallsFile, in
	// first-seen order. It is what the per-method breakdown is keyed on for a
	// calls-file run, since the file's names need not match the declared calls.
	CallsFileMethods []string `yaml:"-"`
}

// UsesCallsFile reports whether the run's traffic comes from a pre-generated
// requests CSV rather than from the declared calls.
func (c *Config) UsesCallsFile() bool {
	return c.CallsFile != "" && len(c.CallsFileMethods) > 0
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
	} else {
		// Read the file now rather than letting k6 discover a bad path minutes
		// into the run, and record the methods the per-method export keys on.
		methods, err := LoadCallsFileMethods(cfg.CallsFile)
		if err != nil {
			return err
		}
		cfg.CallsFileMethods = methods
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
