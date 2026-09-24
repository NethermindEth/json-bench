// Package review turns probe output directories into verified, comparable
// freshness results. It never needs the tested nodes: only the probe files
// and a reference node (or its cached answers).
package review

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/freshness/schema"
)

type Config struct {
	SchemaVersion   int             `yaml:"schema_version" json:"schema_version"`
	Reference       ReferenceConfig `yaml:"reference" json:"reference"`
	Probes          []string        `yaml:"probes" json:"probes"`
	HoldConstant    []string        `yaml:"hold_constant" json:"hold_constant,omitempty"`
	MarginMs        float64         `yaml:"margin_ms" json:"margin_ms"`
	DeadlinesMs     []float64       `yaml:"deadlines_ms" json:"deadlines_ms"`
	IncludeWarmup   bool            `yaml:"include_warmup" json:"include_warmup"`
	TimelineBlocks  int             `yaml:"timeline_blocks" json:"timeline_blocks"`
	OutputDirectory string          `yaml:"output_directory" json:"output_directory"`
}

type ReferenceConfig struct {
	RPCURL           string            `yaml:"rpc_url" json:"rpc_url"`
	Headers          map[string]string `yaml:"headers" json:"headers,omitempty"`
	RequestTimeoutMs int               `yaml:"request_timeout_ms" json:"request_timeout_ms"`
	Concurrency      int               `yaml:"concurrency" json:"concurrency"`
}

func LoadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	expanded, err := config.SubstituteEnvVars(string(raw))
	if err != nil {
		return nil, err
	}
	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader([]byte(expanded)))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	cfg.ApplyDefaults()
	return &cfg, nil
}

func (c *Config) ApplyDefaults() {
	if c.SchemaVersion == 0 {
		c.SchemaVersion = schema.Version
	}
	if c.MarginMs == 0 {
		c.MarginMs = 5
	}
	if len(c.DeadlinesMs) == 0 {
		c.DeadlinesMs = []float64{250, 500, 1000, 2000, 4000}
	}
	if c.TimelineBlocks == 0 {
		c.TimelineBlocks = 10
	}
	if c.Reference.RequestTimeoutMs == 0 {
		c.Reference.RequestTimeoutMs = 10000
	}
	if c.Reference.Concurrency == 0 {
		c.Reference.Concurrency = 8
	}
	if c.OutputDirectory == "" {
		c.OutputDirectory = "results/rpc-freshness/review"
	}
}

// Validate checks the config. offline runs need no reference URL.
func (c *Config) Validate(offline bool) error {
	if c.SchemaVersion != schema.Version {
		return fmt.Errorf("schema_version %d is not supported (want %d)", c.SchemaVersion, schema.Version)
	}
	if len(c.Probes) == 0 {
		return fmt.Errorf("probes: list at least one probe output directory")
	}
	if !offline && c.Reference.RPCURL == "" {
		return fmt.Errorf("reference.rpc_url is required unless --offline")
	}
	if c.MarginMs < 0 {
		return fmt.Errorf("margin_ms must be >= 0")
	}
	return nil
}
