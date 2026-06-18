package config

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

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
