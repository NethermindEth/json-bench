package comparator

import (
	"fmt"
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
