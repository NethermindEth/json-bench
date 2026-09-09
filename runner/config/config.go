package config

import (
	"fmt"
	"time"

	"github.com/jsonrpc-bench/runner/types"
)

// Stage is one leg of a ramping load profile: the arrival rate moves linearly
// from wherever the previous stage left it to Target over Duration. Holding a
// rate is a stage whose Target equals the previous one. This follows k6's
// ramping-arrival-rate shape, with `rps` as the starting rate.
type Stage struct {
	Duration string `yaml:"duration"`
	Target   int    `yaml:"target"`
}

// Config represents the benchmark configuration
type Config struct {
	TestName    string   `yaml:"test_name"`
	Description string   `yaml:"description"`
	ClientRefs  []string `yaml:"clients"`
	Duration    string   `yaml:"duration"`
	RPS         int      `yaml:"rps"`
	Iterations  int      `yaml:"iterations"`
	VUs         int      `yaml:"vus"`

	// Warmup runs load for this long before the measured window opens, and its
	// requests are excluded from every reported statistic. Without it the first
	// seconds of a run — cold caches, an empty connection pool, a JIT that has
	// not compiled anything yet — are averaged into the steady-state
	// percentiles the run exists to report.
	Warmup string `yaml:"warmup"`

	// BatchSize groups consecutive requests into JSON-RPC batch arrays, one
	// HTTP round trip each. `rps` stays a rate of requests, so batches are
	// issued at rps/BatchSize — which is what makes two runs at different batch
	// sizes comparable, since both offer the node the same work.
	BatchSize int `yaml:"batch_size"`

	// Stages ramp the arrival rate rather than holding one. When set, the run's
	// length is the sum of the stages and `duration` must be omitted, so there
	// is only ever one statement of how long the run is.
	Stages          []Stage               `yaml:"stages"`
	Seed            int64                 `yaml:"seed"` // Optional: fixes the request sequence so repeated runs replay identical requests
	Calls           []*Call               `yaml:"calls"`
	CallsFile       string                `yaml:"calls_file"` // Optional: use file containing RPC calls instead of generating them
	ResolvedClients []*types.ClientConfig `yaml:"-"`
	Outputs         *Outputs              `yaml:"-"`

	// CallsFileMethods and CallsFileNames hold the distinct RPC methods and
	// request names found in CallsFile, in first-seen order. Both matter: a
	// profile can drive one method through many named parameter shapes, and the
	// name is then the only thing telling them apart.
	CallsFileMethods []string `yaml:"-"`
	CallsFileNames   []string `yaml:"-"`
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
		// Read the file now rather than discovering a bad path minutes into the
		// run, and record the labels the breakdown keys on.
		methods, names, err := LoadCallsFileLabels(cfg.CallsFile)
		if err != nil {
			return err
		}
		cfg.CallsFileMethods = methods
		cfg.CallsFileNames = names
	}

	if err := validateLoadShape(cfg); err != nil {
		return err
	}

	if cfg.VUs <= 0 {
		return fmt.Errorf("vus must be greater than 0")
	}

	if cfg.BatchSize < 0 {
		return fmt.Errorf("batch_size cannot be negative")
	}
	if cfg.BatchSize > 0 && cfg.Iterations > 0 {
		// Iterations counts requests, batching groups them; which of the two a
		// number refers to would be ambiguous.
		return fmt.Errorf("batch_size cannot be combined with iterations")
	}

	return nil
}

// validateLoadShape checks the three ways a run can state its load — a rate for
// a duration, a fixed number of iterations, or a ramp through stages — and
// rejects combinations that state it twice or not at all.
func validateLoadShape(cfg *Config) error {
	if len(cfg.Stages) > 0 {
		if cfg.Duration != "" {
			return fmt.Errorf("stages set the run's length, so duration must be omitted")
		}
		if cfg.Iterations > 0 {
			return fmt.Errorf("stages ramp a rate over time, so they cannot be combined with iterations")
		}
		for i, stage := range cfg.Stages {
			d, err := time.ParseDuration(stage.Duration)
			if err != nil {
				return fmt.Errorf("stage %d has an invalid duration %q: %w", i+1, stage.Duration, err)
			}
			if d <= 0 {
				return fmt.Errorf("stage %d must have a positive duration", i+1)
			}
			if stage.Target < 0 {
				return fmt.Errorf("stage %d has a negative target rate", i+1)
			}
		}
		if cfg.RPS < 0 {
			return fmt.Errorf("rps is the rate the first stage ramps from and cannot be negative")
		}
		return validateWarmup(cfg)
	}

	if cfg.Duration == "" {
		return fmt.Errorf("duration is required")
	}
	if _, err := time.ParseDuration(cfg.Duration); err != nil {
		return fmt.Errorf("invalid duration format: %w", err)
	}

	if cfg.Iterations > 0 && cfg.RPS > 0 {
		return fmt.Errorf("iterations and rps cannot be used together")
	}
	if cfg.Iterations <= 0 && cfg.RPS <= 0 {
		return fmt.Errorf("either iterations or rps must be greater than 0")
	}

	return validateWarmup(cfg)
}

func validateWarmup(cfg *Config) error {
	if cfg.Warmup == "" {
		return nil
	}
	// Warmup discards the requests issued before a point in time, which needs
	// the load to be paced by time in the first place.
	if cfg.Iterations > 0 {
		return fmt.Errorf("warmup discards a period of the run, so it cannot be combined with iterations")
	}
	d, err := time.ParseDuration(cfg.Warmup)
	if err != nil {
		return fmt.Errorf("invalid warmup format: %w", err)
	}
	if d <= 0 {
		return fmt.Errorf("warmup must be a positive duration")
	}
	return nil
}
