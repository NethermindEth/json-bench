package comparator

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jsonrpc-bench/runner/schema"
	"github.com/jsonrpc-bench/runner/types"
)

// ComparisonResult represents the result of comparing responses from different clients
type ComparisonResult struct {
	Method       string                 `json:"method"`
	Params       []interface{}          `json:"params"`
	Timestamp    string                 `json:"timestamp"`
	Responses    map[string]interface{} `json:"responses"`
	Differences  map[string]interface{} `json:"differences"`
	SchemaErrors map[string][]string    `json:"schema_errors,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// ComparisonConfig represents the configuration for response comparison
type ComparisonConfig struct {
	Name                  string                   `json:"name"`
	Description           string                   `json:"description"`
	Methods               []string                 `json:"methods"`
	Clients               []*types.ClientConfig    `json:"clients"`
	ValidateAgainstSchema bool                     `json:"validate_against_schema"`
	OutputDir             string                   `json:"output_dir"`
	TimeoutSeconds        int                      `json:"timeout_seconds"`
	Concurrency           int                      `json:"concurrency"`
	CustomParameters      map[string][]interface{} `json:"custom_parameters,omitempty"`
	Verbose               bool                     `json:"verbose,omitempty"`
}

// Comparator handles comparing responses between different Ethereum clients
type Comparator struct {
	config    *ComparisonConfig
	validator *schema.SchemaValidator
	outputDir string
	mutex     sync.Mutex
	results   []ComparisonResult
	verbose   bool
}

// NewComparator creates a new response comparator
func NewComparator(cfg *ComparisonConfig) (*Comparator, error) {
	// Create output directory if it doesn't exist
	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create schema validator if needed
	var validator *schema.SchemaValidator
	var err error
	if cfg.ValidateAgainstSchema {
		validator, err = schema.NewSchemaValidator()
		if err != nil {
			return nil, fmt.Errorf("failed to create schema validator: %w", err)
		}
	}

	// Set default concurrency if not specified
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 5
	}

	// Set default timeout if not specified
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = 10 // 10 seconds
	}

	return &Comparator{
		config:    cfg,
		validator: validator,
		outputDir: cfg.OutputDir,
		results:   make([]ComparisonResult, 0),
		verbose:   cfg.Verbose,
	}, nil
}

// CompareResponses compares responses from different clients for a specific method and parameters
func (c *Comparator) CompareResponses(method string, params []interface{}) (*ComparisonResult, error) {
	responses := make(map[string]interface{})
	schemaErrors := make(map[string][]string)

	// Extract base method name if this is a variant (e.g., eth_call_variant1 -> eth_call)
	rpcMethod := method
	if idx := strings.Index(method, "_variant"); idx > 0 {
		rpcMethod = method[:idx]
	}

	// Make JSON-RPC calls to all clients
	for _, client := range c.config.Clients {
		response, err := makeJSONRPCCall(client.URL, rpcMethod, params, c.config.TimeoutSeconds, c.verbose)
		if err != nil {
			return nil, fmt.Errorf("failed to call %s on %s: %w", rpcMethod, client.Name, err)
		}

		responses[client.Name] = response

		// Validate response against schema if enabled
		if c.config.ValidateAgainstSchema && c.validator != nil {
			// Use the base method name for schema validation
			valid, errors, err := c.validator.ValidateResponse(rpcMethod, response)
			if err != nil {
				return nil, fmt.Errorf("schema validation error for %s on %s: %w", rpcMethod, client.Name, err)
			}

			if !valid && len(errors) > 0 {
				schemaErrors[client.Name] = errors
			}
		}
	}

	// Compare responses between clients
	differences := make(map[string]interface{})
	if len(c.config.Clients) >= 2 {
		// Use first client as reference
		refClient := c.config.Clients[0].Name
		refResponse := responses[refClient].(map[string]interface{})

		// Compare with other clients
		for _, client := range c.config.Clients[1:] {
			clientResponse := responses[client.Name].(map[string]interface{})
			diff, err := compareJSONRPCResponses(refResponse, clientResponse)
			if err != nil {
				return nil, fmt.Errorf("failed to compare responses: %w", err)
			}

			if diff != nil && len(diff) > 0 {
				differences[client.Name] = diff
			}
		}
	}

	// Create comparison result
	result := &ComparisonResult{
		Method:       method,
		Params:       params,
		Timestamp:    time.Now().Format(time.RFC3339),
		Responses:    responses,
		Differences:  differences,
		SchemaErrors: schemaErrors,
		Metadata: map[string]interface{}{
			"clients": c.config.Clients,
		},
	}

	// Save result
	c.mutex.Lock()
	c.results = append(c.results, *result)
	c.mutex.Unlock()

	return result, nil
}

// VerifyNetworkConsistency checks if all clients are on the same network by comparing eth_chainId
func (c *Comparator) VerifyNetworkConsistency() error {
	// Skip if there's only one client
	if len(c.config.Clients) <= 1 {
		return nil
	}

	// Get chainId from all clients
	chainIDs := make(map[string]string)
	for _, client := range c.config.Clients {
		response, err := makeJSONRPCCall(client.URL, "eth_chainId", []interface{}{}, c.config.TimeoutSeconds, c.verbose)
		if err != nil {
			return fmt.Errorf("failed to get chainId from %s: %w", client.Name, err)
		}

		// Extract chainId from response
		result, ok := response["result"]
		if !ok {
			return fmt.Errorf("invalid response from %s: missing result field", client.Name)
		}

		chainIDStr, ok := result.(string)
		if !ok {
			return fmt.Errorf("invalid chainId from %s: expected string, got %T", client.Name, result)
		}

		chainIDs[client.Name] = chainIDStr
	}

	// Check if all chainIds are the same
	var referenceChainID string
	var referenceClient string
	for client, chainID := range chainIDs {
		if referenceChainID == "" {
			referenceChainID = chainID
			referenceClient = client
		} else if chainID != referenceChainID {
			return fmt.Errorf("network mismatch: %s has chainId %s, but %s has chainId %s",
				referenceClient, referenceChainID, client, chainID)
		}
	}

	if c.verbose {
		log.Printf("Network consistency verified: all clients are on chainId %s", referenceChainID)
	}

	return nil
}

// RunComparisons runs comparisons for all configured methods
func (c *Comparator) RunComparisons() ([]ComparisonResult, error) {
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, c.config.Concurrency)
	errCh := make(chan error, len(c.config.Methods))

	for _, method := range c.config.Methods {
		// Get parameters for this method
		var params []interface{}
		if customParams, ok := c.config.CustomParameters[method]; ok && len(customParams) > 0 {
			params = customParams
		} else {
			// Use default parameters based on method
			params = getDefaultParams(method)
		}

		wg.Add(1)
		go func(method string, params []interface{}) {
			defer wg.Done()

			// Acquire semaphore
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			_, err := c.CompareResponses(method, params)
			if err != nil {
				errCh <- fmt.Errorf("comparison failed for %s: %w", method, err)
			}
		}(method, params)
	}

	// Wait for all comparisons to complete
	wg.Wait()
	close(errCh)

	// Check for errors
	var errors []string
	for err := range errCh {
		errors = append(errors, err.Error())
	}

	if len(errors) > 0 {
		return c.results, fmt.Errorf("some comparisons failed: %s", strings.Join(errors, "; "))
	}

	return c.results, nil
}

// Run runs the comparator
func (c *Comparator) Run() ([]ComparisonResult, error) {
	// Verify all clients are on the same network
	if err := c.VerifyNetworkConsistency(); err != nil {
		return nil, err
	}

	// Run comparisons
	results, err := c.RunComparisons()
	if err != nil {
		return nil, err
	}

	return results, nil
}

// SaveResults saves comparison results to a JSON file
func (c *Comparator) SaveResults(filename string) error {
	// Use the provided filename directly as it should already be a full path
	outputPath := filename

	// Create directory if it doesn't exist
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Marshal results to JSON
	data, err := json.MarshalIndent(c.results, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal results: %w", err)
	}

	// Write to file
	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write results: %w", err)
	}

	return nil
}

// GenerateReport generates an HTML report from comparison results
func (c *Comparator) GenerateReport(outputPath string) error {
	// This will be implemented in a separate file
	return nil
}

// GetResults returns all comparison results
func (c *Comparator) GetResults() []ComparisonResult {
	return c.results
}

// Helper functions for default parameters

// getDefaultParams returns default parameters for common Ethereum JSON-RPC methods
func getDefaultParams(method string) []interface{} {
	switch method {
	case "eth_blockNumber":
		return []interface{}{}
	case "eth_getBalance":
		return []interface{}{"0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045", "latest"}
	case "eth_call":
		return []interface{}{
			map[string]interface{}{
				"to":   "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2",
				"data": "0x70a08231000000000000000000000000000000000000000000000000000000000000000a",
			},
			"latest",
		}
	case "eth_getBlockByNumber":
		return []interface{}{"latest", false}
	case "eth_getTransactionCount":
		return []interface{}{"0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045", "latest"}
	case "eth_chainId":
		return []interface{}{}
	case "eth_gasPrice":
		return []interface{}{}
	case "net_version":
		return []interface{}{}
	default:
		return []interface{}{}
	}
}
