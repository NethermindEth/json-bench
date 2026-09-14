package config

import (
	"fmt"
	"math/rand"
)

// RPCCall represents a single JSON-RPC call
type RPCCall struct {
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
}

// Call represents a JSON-RPC method call to benchmark
type Call struct {
	Name     string        `yaml:"name"` // Custom name for this call or calls collection
	Method   string        `yaml:"method"`
	Params   []interface{} `yaml:"params"`
	Weight   int           `yaml:"weight"`
	Calls    []RPCCall     `yaml:"calls,omitempty"`
	File     string        `yaml:"file,omitempty"`      // Optional: file containing RPC calls
	FileType string        `yaml:"file_type,omitempty"` // Type of file: "json" or "jsonl"
	// Thresholds are pass/fail conditions on this call's latency, written as
	// "p(99)<600" in milliseconds. Supported statistics: p(50|75|90|95|99|99.9),
	// avg, min, max, med, count, and rate for the error ratio.
	Thresholds []string `yaml:"thresholds,omitempty"`
}

// LoadFile loads calls from a file
func (c *Call) LoadFile() error {
	if c.File == "" {
		return nil // No file to load
	}

	calls, err := LoadCallsFromFile(c.File, c.FileType)
	if err != nil {
		return err
	}

	c.Calls = calls
	return nil
}

// Sample returns a call drawn uniformly from the collection with rng, or the single call if no collection is provided
func (c *Call) Sample(rng *rand.Rand) (RPCCall, error) {
	if len(c.Calls) > 0 {
		return c.Calls[rng.Intn(len(c.Calls))], nil
	}

	// Use the single call if no collection is provided
	if c.Method == "" || c.Params == nil {
		return RPCCall{}, fmt.Errorf("invalid call: no method or params provided")
	}

	return RPCCall{
		Method: c.Method,
		Params: c.Params,
	}, nil
}
