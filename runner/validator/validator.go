package validator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jsonrpc-bench/runner/schema"
	"github.com/jsonrpc-bench/runner/types"
)

// ResponseDiff is an alias for types.ResponseDiff
type ResponseDiff = types.ResponseDiff

// ValidateResponses compares responses from different clients and returns differences
func ValidateResponses(result *types.BenchmarkResult) ([]ResponseDiff, error) {
	if result == nil || result.ResponsesDir == "" {
		return nil, fmt.Errorf("no responses directory provided")
	}

	// Create schema validator
	validator, err := schema.NewSchemaValidator()
	if err != nil {
		return nil, fmt.Errorf("failed to create schema validator: %w", err)
	}

	// Get list of supported methods for schema validation
	supportedMethods := validator.GetSupportedMethods()
	supportedMethodsMap := make(map[string]bool)
	for _, method := range supportedMethods {
		supportedMethodsMap[method] = true
	}

	// Read response files from the responses directory
	responseFiles, err := filepath.Glob(filepath.Join(result.ResponsesDir, "*.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to list response files: %w", err)
	}

	// Group responses by method and request ID
	responsesByMethodAndID := make(map[string]map[string]map[string]interface{})
	for _, file := range responseFiles {
		// Parse filename to extract client, method, and request ID
		basename := filepath.Base(file)
		parts := strings.Split(strings.TrimSuffix(basename, ".json"), "_")
		if len(parts) < 3 {
			continue // Invalid filename format
		}

		client := parts[0]
		method := parts[1]
		// We need the requestID for grouping responses
		requestID := parts[2]

		// Read and parse response file
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("failed to read response file %s: %w", file, err)
		}

		var response map[string]interface{}
		if err := json.Unmarshal(data, &response); err != nil {
			return nil, fmt.Errorf("failed to parse response file %s: %w", file, err)
		}

		// Group by method and request ID
		if _, ok := responsesByMethodAndID[method]; !ok {
			responsesByMethodAndID[method] = make(map[string]map[string]interface{})
		}
		if _, ok := responsesByMethodAndID[method][requestID]; !ok {
			responsesByMethodAndID[method][requestID] = make(map[string]interface{})
		}
		responsesByMethodAndID[method][requestID][client] = response
	}

	// Compare responses and validate against schemas
	var diffs []ResponseDiff
	for method, requestIDs := range responsesByMethodAndID {
		for requestID, clientResponses := range requestIDs {
			// Log the request ID for debugging purposes
			fmt.Printf("Processing responses for method %s with request ID %s\n", method, requestID)
			if len(clientResponses) < 2 {
				continue // Need at least 2 clients to compare
			}

			// Extract clients and responses
			clients := make([]string, 0, len(clientResponses))
			responses := make([]map[string]interface{}, 0, len(clientResponses))
			for client, resp := range clientResponses {
				clients = append(clients, client)
				responses = append(responses, resp.(map[string]interface{}))
			}

			// Compare responses between clients
			differences, err := CompareResponses(responses[0], responses[1])
			if err != nil {
				return nil, fmt.Errorf("failed to compare responses for method %s: %w", method, err)
			}

			// Create response diff
			diff := ResponseDiff{
				Method:       method,
				Params:       nil, // We don't have the params in this implementation
				Clients:      clients,
				Differences:  differences,
				SchemaErrors: make(map[string][]string),
			}

			// Validate against schema if method is supported
			if supportedMethodsMap[method] {
				for client, resp := range clientResponses {
					valid, errors, err := validator.ValidateResponse(method, resp.(map[string]interface{}))
					if err != nil {
						return nil, fmt.Errorf("schema validation failed for method %s: %w", method, err)
					}

					if !valid && len(errors) > 0 {
						diff.SchemaErrors[client] = errors
					}
				}
			}

			// Add diff if there are differences or schema errors
			if (differences != nil && len(differences) > 0) || len(diff.SchemaErrors) > 0 {
				diffs = append(diffs, diff)
			}
		}
	}

	return diffs, nil
}

// CompareResponses compares two JSON-RPC responses and returns their differences
func CompareResponses(resp1, resp2 map[string]interface{}) (map[string]interface{}, error) {
	// This is a simplified implementation
	// In a real-world scenario, we would do a deep comparison of the response objects

	// Check if both have result or error fields
	result1, hasResult1 := resp1["result"]
	result2, hasResult2 := resp2["result"]

	error1, hasError1 := resp1["error"]
	error2, hasError2 := resp2["error"]

	differences := make(map[string]interface{})

	// Check for inconsistent error/result presence
	if hasResult1 != hasResult2 {
		differences["result_presence"] = "inconsistent"
	}

	if hasError1 != hasError2 {
		differences["error_presence"] = "inconsistent"
	}

	// If both have results, compare them
	if hasResult1 && hasResult2 {
		// For simple types, direct comparison
		switch result1.(type) {
		case string, bool, float64, int, int64:
			if result1 != result2 {
				differences["result"] = "different values"
			}

		case []interface{}:
			// For arrays, compare length first
			arr1 := result1.([]interface{})
			arr2, ok := result2.([]interface{})
			if !ok || len(arr1) != len(arr2) {
				differences["result"] = "different array length or type"
			}

		case map[string]interface{}:
			// For objects, compare keys
			obj1 := result1.(map[string]interface{})
			obj2, ok := result2.(map[string]interface{})
			if !ok {
				differences["result"] = "different types"
			} else {
				// Check if all keys in obj1 exist in obj2
				for key := range obj1 {
					if _, exists := obj2[key]; !exists {
						if differences["missing_fields"] == nil {
							differences["missing_fields"] = []string{}
						}
						differences["missing_fields"] = append(differences["missing_fields"].([]string), key)
					}
				}

				// Check if all keys in obj2 exist in obj1
				for key := range obj2 {
					if _, exists := obj1[key]; !exists {
						if differences["extra_fields"] == nil {
							differences["extra_fields"] = []string{}
						}
						differences["extra_fields"] = append(differences["extra_fields"].([]string), key)
					}
				}
			}
		}
	}

	// If both have errors, compare them
	if hasError1 && hasError2 {
		// Compare error codes
		errorObj1, ok1 := error1.(map[string]interface{})
		errorObj2, ok2 := error2.(map[string]interface{})

		if !ok1 || !ok2 {
			differences["error"] = "different error formats"
		} else {
			code1, hasCode1 := errorObj1["code"]
			code2, hasCode2 := errorObj2["code"]

			if hasCode1 != hasCode2 || code1 != code2 {
				differences["error_code"] = "different error codes"
			}
		}
	}

	if len(differences) == 0 {
		return nil, nil
	}

	return differences, nil
}
