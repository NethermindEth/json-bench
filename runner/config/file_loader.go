package config

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// RPCCall represents a single JSON-RPC call
type RPCCall struct {
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
}

// LoadCallsFromFile loads RPC calls from a file
func LoadCallsFromFile(filePath string, fileType string) ([]RPCCall, error) {
	if fileType == "" {
		// Auto-detect file type based on extension
		ext := filepath.Ext(filePath)
		switch ext {
		case ".json":
			fileType = "json"
		case ".jsonl":
			fileType = "jsonl"
		default:
			return nil, fmt.Errorf("unable to determine file type for %s", filePath)
		}
	}

	switch fileType {
	case "json":
		return loadCallsFromJSON(filePath)
	case "jsonl":
		return loadCallsFromJSONL(filePath)
	default:
		return nil, fmt.Errorf("unsupported file type: %s", fileType)
	}
}

// loadCallsFromJSON loads RPC calls from a JSON file
func loadCallsFromJSON(filePath string) ([]RPCCall, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var calls []RPCCall
	if err := json.Unmarshal(data, &calls); err != nil {
		// Try to unmarshal as a single call
		var singleCall RPCCall
		if err := json.Unmarshal(data, &singleCall); err != nil {
			return nil, fmt.Errorf("failed to parse JSON: %w", err)
		}
		calls = []RPCCall{singleCall}
	}

	return calls, nil
}

// loadCallsFromJSONL loads RPC calls from a JSONL (JSON Lines) file
func loadCallsFromJSONL(filePath string) ([]RPCCall, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	var calls []RPCCall
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if line == "" {
			continue // Skip empty lines
		}

		var call RPCCall
		if err := json.Unmarshal([]byte(line), &call); err != nil {
			return nil, fmt.Errorf("failed to parse line %d: %w", lineNum, err)
		}
		calls = append(calls, call)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading file: %w", err)
	}

	return calls, nil
}

// ExpandMethodsWithFiles expands methods that reference files into individual calls
func ExpandMethodsWithFiles(methods []Method) ([]Method, error) {
	var expandedMethods []Method

	for _, method := range methods {
		if method.File != "" {
			// Load calls from file
			calls, err := LoadCallsFromFile(method.File, method.FileType)
			if err != nil {
				return nil, fmt.Errorf("failed to load calls from %s: %w", method.File, err)
			}

			// Create a method for each call, distributing the weight
			if len(calls) > 0 {
				// Parse frequency percentage
				totalWeight := method.Weight

				// Distribute frequency among calls
				baseWeight := totalWeight / len(calls)
				remainder := totalWeight % len(calls)

				for i, call := range calls {
					// Add 1 to the first 'remainder' calls to handle rounding
					callWeight := baseWeight
					if i < remainder {
						callWeight++
					}

					expandedMethods = append(expandedMethods, Method{
						Name:       fmt.Sprintf("%s-%d", method.Name, i), // Add a unique name to the expanded method
						Method:     call.Method,
						Params:     call.Params,
						Weight:     callWeight,
						Thresholds: method.Thresholds, // Expanded methods inherit thresholds from the original method
					})
				}
			}
		} else {
			// Regular method without file
			expandedMethods = append(expandedMethods, method)
		}
	}

	return expandedMethods, nil
}
