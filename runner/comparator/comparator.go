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

// DefaultDiffOnlyMaxResponseBytes is the response-body cap applied when
// --diff-only is used without an explicit body-trimming flag, so the report
// stays small by default. Callers can opt out to retain full bodies.
const DefaultDiffOnlyMaxResponseBytes = 4096

// ComparisonResult represents the result of comparing responses from different clients
type ComparisonResult struct {
	Method          string                 `json:"method"`
	Params          []interface{}          `json:"params"`
	Timestamp       string                 `json:"timestamp"`
	Responses       map[string]interface{} `json:"responses"`
	Differences     map[string]interface{} `json:"differences"`
	SchemaErrors    map[string][]string    `json:"schema_errors,omitempty"`
	TransportErrors map[string]string      `json:"transport_errors,omitempty"`
	ErrorClass      map[string]string      `json:"error_class,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
}

// hasDifferences reports whether the call has any post-filter differences.
func (r ComparisonResult) hasDifferences() bool {
	return len(r.Differences) > 0
}

// isIdentical reports whether every client agreed and nothing went wrong: no
// post-filter differences, no transport failures, and no schema errors.
func (r ComparisonResult) isIdentical() bool {
	return len(r.Differences) == 0 && len(r.TransportErrors) == 0 && len(r.SchemaErrors) == 0
}

// isEnvDifference reports whether a differing call's difference stems from an
// environment/capability error (see classifyError) on some client rather than
// a real result mismatch. A response is either a result or an error, so a
// classified error on any client means the difference is that error, not a
// correctness regression.
func (r ComparisonResult) isEnvDifference() bool {
	return r.hasDifferences() && len(r.ErrorClass) > 0
}

// ComparisonConfig represents the configuration for response comparison.
//
// Methods holds internal identifiers (e.g. "eth_getBalance_variant1" or
// "eth_getBalance_vitalik-latest"). MethodRPCNames maps each identifier to
// the JSON-RPC method actually invoked on the wire; when unset, the
// identifier is used as-is.
type ComparisonConfig struct {
	Name                  string                   `json:"name"`
	Description           string                   `json:"description"`
	Methods               []string                 `json:"methods"`
	MethodRPCNames        map[string]string        `json:"method_rpc_names,omitempty"`
	Clients               []*types.ClientConfig    `json:"clients"`
	ValidateAgainstSchema bool                     `json:"validate_against_schema"`
	OutputDir             string                   `json:"output_dir"`
	TimeoutSeconds        int                      `json:"timeout_seconds"`
	Concurrency           int                      `json:"concurrency"`
	CustomParameters      map[string][]interface{} `json:"custom_parameters,omitempty"`
	Verbose               bool                     `json:"verbose,omitempty"`

	// Rules declare expected differences (see ComparisonRule). BlockOverride,
	// when set, rewrites latest/pending block tags to a static block and
	// appends a block argument to calls that omit one.
	Rules         []ComparisonRule `json:"rules,omitempty"`
	BlockOverride string           `json:"block_override,omitempty"`

	// MaxRetries and RetryBaseDelayMs bound transport retries. When unset they
	// fall back to the per-client ClientConfig.MaxRetries and then to defaults
	// (5 attempts, 200ms base delay).
	MaxRetries       int `json:"max_retries,omitempty"`
	RetryBaseDelayMs int `json:"retry_base_delay_ms,omitempty"`

	// DiffOnly excludes identical calls from serialized output.
	// OmitMatchingResponses drops full responses (diffs still carry the
	// differing values). MaxResponseBytes truncates embedded response bodies.
	DiffOnly              bool `json:"diff_only,omitempty"`
	OmitMatchingResponses bool `json:"omit_matching_responses,omitempty"`
	MaxResponseBytes      int  `json:"max_response_bytes,omitempty"`

	// SkipAboveHead skips calls pinned to a numeric block above the lowest
	// client head.
	SkipAboveHead bool `json:"skip_above_head,omitempty"`
}

// Comparator handles comparing responses between different Ethereum clients
type Comparator struct {
	config    *ComparisonConfig
	validator *schema.SchemaValidator
	outputDir string
	mutex     sync.Mutex
	results   []ComparisonResult
	skipped   []skippedCall
	verbose   bool
}

// skippedCall records a call omitted because it pins to a block above the
// lowest client head (see --skip-above-head).
type skippedCall struct {
	Method string `json:"method"`
	Reason string `json:"reason"`
	Block  string `json:"block,omitempty"`
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
	transportErrors := make(map[string]string)
	errorClass := make(map[string]string)

	// Recover the real JSON-RPC method name from the identifier; loaders set
	// MethodRPCNames to point each identifier (e.g. eth_call_variant1) at the
	// wire-level method (eth_call). Fall back to the identifier itself so
	// callers that pass a flat []string still work.
	rpcMethod := method
	if name, ok := c.config.MethodRPCNames[method]; ok && name != "" {
		rpcMethod = name
	}

	callParams := params
	if c.config.BlockOverride != "" {
		callParams = applyBlockOverride(rpcMethod, params, c.config.BlockOverride)
	}

	// Make JSON-RPC calls to all clients. A transport failure for one client is
	// recorded and the run continues, so a single dead endpoint never discards
	// an otherwise good comparison.
	answered := make([]*types.ClientConfig, 0, len(c.config.Clients))
	for _, client := range c.config.Clients {
		maxAttempts, baseDelay := c.retryParams(client)
		response, err := makeJSONRPCCall(client.URL, rpcMethod, callParams, c.config.TimeoutSeconds, c.verbose, maxAttempts, baseDelay)
		if err != nil {
			transportErrors[client.Name] = err.Error()
			continue
		}

		responses[client.Name] = response
		answered = append(answered, client)
		if cls := classifyError(response); cls != "" {
			errorClass[client.Name] = cls
		}

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

	// Compare responses between the clients that answered. The reference is the
	// first answered client (normally config.Clients[0]).
	differences := make(map[string]interface{})
	if len(answered) >= 2 {
		refResponse := responses[answered[0].Name].(map[string]interface{})
		ctx := newDiffContext(rpcMethod, c.config.Rules)

		for _, client := range answered[1:] {
			clientResponse := responses[client.Name].(map[string]interface{})
			diff, err := compareJSONRPCResponses(ctx, refResponse, clientResponse)
			if err != nil {
				return nil, fmt.Errorf("failed to compare responses: %w", err)
			}

			if len(diff) > 0 {
				differences[client.Name] = diff
			}
		}
	}

	// Create comparison result
	result := &ComparisonResult{
		Method:          method,
		Params:          params,
		Timestamp:       time.Now().Format(time.RFC3339),
		Responses:       responses,
		Differences:     differences,
		SchemaErrors:    schemaErrors,
		TransportErrors: transportErrors,
		ErrorClass:      errorClass,
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

// retryParams resolves the retry budget: an explicit config value (from the
// --max-retries / --retry-base-delay flags) wins, then the per-client
// ClientConfig.MaxRetries, then the defaults (5 attempts, 200ms base delay).
func (c *Comparator) retryParams(client *types.ClientConfig) (maxAttempts int, baseDelay time.Duration) {
	maxAttempts = c.config.MaxRetries
	if maxAttempts <= 0 && client != nil {
		maxAttempts = client.MaxRetries
	}
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	baseDelay = time.Duration(c.config.RetryBaseDelayMs) * time.Millisecond
	if baseDelay <= 0 {
		baseDelay = 200 * time.Millisecond
	}
	return maxAttempts, baseDelay
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
		maxAttempts, baseDelay := c.retryParams(client)
		response, err := makeJSONRPCCall(client.URL, "eth_chainId", []interface{}{}, c.config.TimeoutSeconds, c.verbose, maxAttempts, baseDelay)
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
		// Loaders populate CustomParameters for every identifier they emit;
		// the empty-slice fallback preserves behaviour for callers that pass
		// bare method names without a matching CustomParameters entry (the
		// OpenRPC loader does this for 0-arg methods and for base names when
		// per-method variations are also present).
		params := c.config.CustomParameters[method]
		if params == nil {
			params = []interface{}{}
		}

		wg.Add(1)
		go func(method string, params []interface{}) {
			defer wg.Done()

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

	if c.config.SkipAboveHead {
		if err := c.applySkipAboveHead(); err != nil {
			return nil, err
		}
	}

	// Run comparisons
	results, err := c.RunComparisons()
	if err != nil {
		return nil, err
	}

	return results, nil
}

// applySkipAboveHead removes methods pinned to a numeric block above the lowest
// client head, so a less-synced client does not produce false differences.
// Hash-addressed calls (e.g. eth_getBlockByHash) cannot be checked numerically
// and are always kept.
func (c *Comparator) applySkipAboveHead() error {
	lowestHead, err := c.lowestHead()
	if err != nil {
		return err
	}
	if c.verbose {
		log.Printf("skip-above-head: lowest client head is %d", lowestHead)
	}

	kept := make([]string, 0, len(c.config.Methods))
	for _, method := range c.config.Methods {
		rpcMethod := method
		if name, ok := c.config.MethodRPCNames[method]; ok && name != "" {
			rpcMethod = name
		}
		block, ok := pinnedBlock(rpcMethod, c.config.CustomParameters[method])
		if ok && block > lowestHead {
			c.skipped = append(c.skipped, skippedCall{
				Method: method,
				Reason: "pinned block above lowest client head",
				Block:  fmt.Sprintf("0x%x", block),
			})
			continue
		}
		kept = append(kept, method)
	}
	c.config.Methods = kept
	return nil
}

// lowestHead returns the minimum eth_blockNumber across all clients.
func (c *Comparator) lowestHead() (uint64, error) {
	var lowest uint64
	first := true
	for _, client := range c.config.Clients {
		maxAttempts, baseDelay := c.retryParams(client)
		resp, err := makeJSONRPCCall(client.URL, "eth_blockNumber", []interface{}{}, c.config.TimeoutSeconds, c.verbose, maxAttempts, baseDelay)
		if err != nil {
			return 0, fmt.Errorf("failed to get head from %s: %w", client.Name, err)
		}
		s, ok := resp["result"].(string)
		if !ok {
			return 0, fmt.Errorf("invalid eth_blockNumber from %s", client.Name)
		}
		head, ok := parseHexBig(s)
		if !ok {
			return 0, fmt.Errorf("invalid eth_blockNumber %q from %s", s, client.Name)
		}
		h := head.Uint64()
		if first || h < lowest {
			lowest = h
			first = false
		}
	}
	return lowest, nil
}

// SaveResults saves comparison results to a JSON file, honoring the diff-only
// and response-trimming output options.
func (c *Comparator) SaveResults(filename string) error {
	// Use the provided filename directly as it should already be a full path
	outputPath := filename

	// Create directory if it doesn't exist
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Marshal results to JSON
	data, err := json.MarshalIndent(c.resultsForOutput(), "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal results: %w", err)
	}

	// Write to file
	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write results: %w", err)
	}

	return nil
}

// resultsForOutput applies the output options (--diff-only,
// --omit-matching-responses, --max-response-bytes) to a copy of the results so
// serialization stays small. The stored results are never mutated.
func (c *Comparator) resultsForOutput() []ComparisonResult {
	out := make([]ComparisonResult, 0, len(c.results))
	for _, r := range c.results {
		if c.config.DiffOnly && r.isIdentical() {
			continue
		}
		if c.config.OmitMatchingResponses {
			r.Responses = nil
		} else if c.config.MaxResponseBytes > 0 {
			r.Responses = truncateResponses(r.Responses, c.config.MaxResponseBytes)
		}
		out = append(out, r)
	}
	return out
}

// truncateResponses replaces any response whose JSON encoding exceeds maxBytes
// with a small placeholder, so large block bodies do not bloat the report.
func truncateResponses(responses map[string]interface{}, maxBytes int) map[string]interface{} {
	if responses == nil {
		return nil
	}
	out := make(map[string]interface{}, len(responses))
	for name, resp := range responses {
		encoded, err := json.Marshal(resp)
		if err == nil && len(encoded) > maxBytes {
			out[name] = map[string]interface{}{
				"_truncated": true,
				"_bytes":     len(encoded),
			}
			continue
		}
		out[name] = resp
	}
	return out
}

// Summary tallies the results by outcome category. Differ counts real result
// mismatches; DifferEnv counts mismatches attributable to an environment or
// capability error (see classifyError).
type Summary struct {
	Total          int
	Identical      int
	Differ         int
	DifferEnv      int
	TransportError int
	SchemaError    int
	EnvError       map[string]int
	Skipped        int
}

// Summarize computes the outcome tally for the completed run.
func (c *Comparator) Summarize() Summary {
	s := Summary{Total: len(c.results), EnvError: map[string]int{}, Skipped: len(c.skipped)}
	for _, r := range c.results {
		switch {
		case r.hasDifferences():
			if r.isEnvDifference() {
				s.DifferEnv++
			} else {
				s.Differ++
			}
		case len(r.TransportErrors) > 0:
			s.TransportError++
		case len(r.SchemaErrors) > 0:
			s.SchemaError++
		default:
			s.Identical++
		}
		for _, cls := range r.ErrorClass {
			s.EnvError[cls]++
		}
	}
	return s
}

// HasDifferences reports whether any call has post-filter differences,
// including environment/expected ones.
func (c *Comparator) HasDifferences() bool {
	for _, r := range c.results {
		if r.hasDifferences() {
			return true
		}
	}
	return false
}

// HasRealDifferences reports whether any call has a real result mismatch (not
// attributable to an environment/capability error). This is what --fail-on-diff
// trips on by default.
func (c *Comparator) HasRealDifferences() bool {
	for _, r := range c.results {
		if r.hasDifferences() && !r.isEnvDifference() {
			return true
		}
	}
	return false
}

// HasEnvDifferences reports whether any differing call is attributable to an
// environment/capability error.
func (c *Comparator) HasEnvDifferences() bool {
	for _, r := range c.results {
		if r.isEnvDifference() {
			return true
		}
	}
	return false
}

// Provenance returns the effective comparison configuration so a report is
// self-describing and reproducible.
func (c *Comparator) Provenance() map[string]interface{} {
	clientRefs := make([]string, 0, len(c.config.Clients))
	for _, client := range c.config.Clients {
		clientRefs = append(clientRefs, client.Name)
	}
	return map[string]interface{}{
		"name":                    c.config.Name,
		"description":             c.config.Description,
		"generated_at":            time.Now().Format(time.RFC3339),
		"client_refs":             clientRefs,
		"block_override":          c.config.BlockOverride,
		"rules":                   c.config.Rules,
		"concurrency":             c.config.Concurrency,
		"timeout_seconds":         c.config.TimeoutSeconds,
		"validate_against_schema": c.config.ValidateAgainstSchema,
		"diff_only":               c.config.DiffOnly,
		"omit_matching_responses": c.config.OmitMatchingResponses,
		"max_response_bytes":      c.config.MaxResponseBytes,
		"skip_above_head":         c.config.SkipAboveHead,
		"call_count":              len(c.config.Methods),
		"skipped":                 c.skipped,
	}
}

// SaveProvenance writes the effective configuration to a sidecar JSON file.
func (c *Comparator) SaveProvenance(filename string) error {
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}
	data, err := json.MarshalIndent(c.Provenance(), "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal provenance: %w", err)
	}
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write provenance: %w", err)
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

// MergeComparisonRules layers rules from a --rules file on top of the rules
// already on the config. Layered rules are evaluated first, so they take
// precedence over config rules on the same path.
func (c *ComparisonConfig) MergeComparisonRules(rules []ComparisonRule) {
	if len(rules) == 0 {
		return
	}
	merged := make([]ComparisonRule, 0, len(rules)+len(c.Rules))
	merged = append(merged, rules...)
	merged = append(merged, c.Rules...)
	c.Rules = merged
}
