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

// makeJSONRPCCall makes a JSON-RPC call to the specified endpoint, retrying
// transport errors and 5xx responses with exponential backoff up to
// maxAttempts. A 200 response carrying a JSON-RPC error object is returned as a
// valid response (not retried); a 4xx is a hard failure (not retried).
func makeJSONRPCCall(url, method string, params []interface{}, timeoutSeconds int, verbose bool, maxAttempts int, baseDelay time.Duration) (map[string]interface{}, error) {
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

	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(backoffDelay(baseDelay, attempt))
		}

		req, err := http.NewRequest("POST", url, bytes.NewBuffer(requestJSON))
		if err != nil {
			return nil, fmt.Errorf("failed to create HTTP request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("HTTP request failed: %w", err)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("failed to read response body: %w", err)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP request failed with status %d: %s", resp.StatusCode, string(body))
			if resp.StatusCode >= 500 {
				continue
			}
			return nil, lastErr
		}

		var rawResponse map[string]interface{}
		if err := json.Unmarshal(body, &rawResponse); err != nil {
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}
		return rawResponse, nil
	}

	return nil, lastErr
}

// backoffDelay returns the exponential backoff for a given (1-based) retry
// attempt: base, 2*base, 4*base, ...
func backoffDelay(base time.Duration, attempt int) time.Duration {
	return base * time.Duration(1<<uint(attempt-1))
}

// BatchJSONRPCRequest represents a batch JSON-RPC request
type BatchJSONRPCRequest []JSONRPCRequest

// makeBatchJSONRPCCall makes a batch JSON-RPC call to the specified endpoint,
// retrying transport errors and 5xx responses with exponential backoff.
func makeBatchJSONRPCCall(url string, requests []JSONRPCRequest, timeoutSeconds int, verbose bool, maxAttempts int, baseDelay time.Duration) ([]map[string]interface{}, error) {
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

	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(backoffDelay(baseDelay, attempt))
		}

		req, err := http.NewRequest("POST", url, bytes.NewBuffer(requestJSON))
		if err != nil {
			return nil, fmt.Errorf("failed to create HTTP request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("HTTP request failed: %w", err)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("failed to read response body: %w", err)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP request failed with status %d: %s", resp.StatusCode, string(body))
			if resp.StatusCode >= 500 {
				continue
			}
			return nil, lastErr
		}

		var rawResponses []map[string]interface{}
		if err := json.Unmarshal(body, &rawResponses); err != nil {
			return nil, fmt.Errorf("failed to parse batch response: %w", err)
		}
		return rawResponses, nil
	}

	return nil, lastErr
}
