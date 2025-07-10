package comparator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// JSONRPCRequest represents a JSON-RPC request
type JSONRPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	ID      int           `json:"id"`
}

// JSONRPCResponse represents a JSON-RPC response
type JSONRPCResponse struct {
	JSONRPC string                 `json:"jsonrpc"`
	Result  interface{}            `json:"result,omitempty"`
	Error   *JSONRPCError          `json:"error,omitempty"`
	ID      int                    `json:"id"`
	Raw     map[string]interface{} `json:"-"` // Store the raw response for comparison
}

// JSONRPCError represents a JSON-RPC error
type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// formatCurlCommand formats a JSON-RPC request as a curl command for logging purposes
func formatCurlCommand(url string, requestJSON []byte) string {
	// Use double quotes for JSON payload to avoid shell escaping issues
	return fmt.Sprintf("curl -X POST -H 'Content-Type: application/json' -d %q %s",
		string(requestJSON), url)
}

// makeJSONRPCCall makes a JSON-RPC call to the specified endpoint
func makeJSONRPCCall(url, method string, params []interface{}, timeoutSeconds int, verbose bool) (map[string]interface{}, error) {
	// Create request
	request := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      1,
	}

	// Marshal request to JSON
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Log the equivalent curl command if verbose mode is enabled
	if verbose {
		log.Printf("JSON-RPC Request to %s: %s", url, formatCurlCommand(url, requestJSON))
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: time.Duration(timeoutSeconds) * time.Second,
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(requestJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var rawResponse map[string]interface{}
	if err := json.Unmarshal(body, &rawResponse); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Return raw response for comparison
	return rawResponse, nil
}

// BatchJSONRPCRequest represents a batch JSON-RPC request
type BatchJSONRPCRequest []JSONRPCRequest

// makeBatchJSONRPCCall makes a batch JSON-RPC call to the specified endpoint
func makeBatchJSONRPCCall(url string, requests []JSONRPCRequest, timeoutSeconds int, verbose bool) ([]map[string]interface{}, error) {
	// Marshal request to JSON
	requestJSON, err := json.Marshal(requests)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal batch request: %w", err)
	}

	// Log the equivalent curl command for batch request if verbose mode is enabled
	if verbose {
		log.Printf("Batch JSON-RPC Request to %s: %s", url, formatCurlCommand(url, requestJSON))
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: time.Duration(timeoutSeconds) * time.Second,
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(requestJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var rawResponses []map[string]interface{}
	if err := json.Unmarshal(body, &rawResponses); err != nil {
		return nil, fmt.Errorf("failed to parse batch response: %w", err)
	}

	// Return raw responses for comparison
	return rawResponses, nil
}
