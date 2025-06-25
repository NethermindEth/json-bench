package comparator

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// DiffType represents the type of difference found
type DiffType string

const (
	// DiffTypeValueMismatch indicates that values are different
	DiffTypeValueMismatch DiffType = "value_mismatch"
	
	// DiffTypeTypeMismatch indicates that types are different
	DiffTypeTypeMismatch DiffType = "type_mismatch"
	
	// DiffTypeFieldMissing indicates that a field is missing in one response
	DiffTypeFieldMissing DiffType = "field_missing"
	
	// DiffTypeFieldExtra indicates that a field is extra in one response
	DiffTypeFieldExtra DiffType = "field_extra"
	
	// DiffTypeArrayLengthMismatch indicates that array lengths are different
	DiffTypeArrayLengthMismatch DiffType = "array_length_mismatch"
)

// DiffEntry represents a single difference between responses
type DiffEntry struct {
	Path      string      `json:"path"`
	Type      DiffType    `json:"type"`
	Value1    interface{} `json:"value1"`
	Value2    interface{} `json:"value2"`
	Reference string      `json:"reference,omitempty"`
}

// compareJSONRPCResponses compares two JSON-RPC responses and returns their differences
func compareJSONRPCResponses(resp1, resp2 map[string]interface{}) (map[string]interface{}, error) {
	differences := make(map[string]interface{})
	
	// Check if both have result or error fields
	result1, hasResult1 := resp1["result"]
	result2, hasResult2 := resp2["result"]
	
	error1, hasError1 := resp1["error"]
	error2, hasError2 := resp2["error"]
	
	// Check for inconsistent error/result presence
	if hasResult1 != hasResult2 {
		differences["result_presence"] = map[string]interface{}{
			"type":  "inconsistent",
			"resp1": hasResult1,
			"resp2": hasResult2,
		}
	}
	
	if hasError1 != hasError2 {
		differences["error_presence"] = map[string]interface{}{
			"type":  "inconsistent",
			"resp1": hasError1,
			"resp2": hasError2,
		}
	}
	
	// If both have results, compare them
	if hasResult1 && hasResult2 {
		resultDiffs, err := deepCompare("result", result1, result2)
		if err != nil {
			return nil, fmt.Errorf("failed to compare results: %w", err)
		}
		
		if len(resultDiffs) > 0 {
			differences["result_differences"] = resultDiffs
		}
	}
	
	// If both have errors, compare them
	if hasError1 && hasError2 {
		errorDiffs, err := deepCompare("error", error1, error2)
		if err != nil {
			return nil, fmt.Errorf("failed to compare errors: %w", err)
		}
		
		if len(errorDiffs) > 0 {
			differences["error_differences"] = errorDiffs
		}
	}
	
	return differences, nil
}

// deepCompare recursively compares two values and returns their differences
func deepCompare(path string, val1, val2 interface{}) ([]DiffEntry, error) {
	// Handle nil values
	if val1 == nil && val2 == nil {
		return nil, nil
	}
	
	if val1 == nil || val2 == nil {
		return []DiffEntry{{
			Path:   path,
			Type:   DiffTypeValueMismatch,
			Value1: val1,
			Value2: val2,
		}}, nil
	}
	
	// Get types
	type1 := reflect.TypeOf(val1)
	type2 := reflect.TypeOf(val2)
	
	// If types are different, report a type mismatch
	if type1 != type2 {
		return []DiffEntry{{
			Path:   path,
			Type:   DiffTypeTypeMismatch,
			Value1: fmt.Sprintf("%T", val1),
			Value2: fmt.Sprintf("%T", val2),
		}}, nil
	}
	
	// Compare based on type
	switch val1.(type) {
	case map[string]interface{}:
		return compareObjects(path, val1.(map[string]interface{}), val2.(map[string]interface{}))
		
	case []interface{}:
		return compareArrays(path, val1.([]interface{}), val2.([]interface{}))
		
	case string, float64, bool, int, int64:
		// Special case for Ethereum hex strings: treat 0x and 0x0000...0000 as equal
		if str1, ok1 := val1.(string); ok1 {
			if str2, ok2 := val2.(string); ok2 {
				// Check if both are hex strings
				if strings.HasPrefix(str1, "0x") && strings.HasPrefix(str2, "0x") {
					// Check if one is 0x and the other is a zero-value hex string
					if (str1 == "0x" && isZeroHex(str2)) || (str2 == "0x" && isZeroHex(str1)) {
						// Consider them equal
						return nil, nil
					}
				}
			}
		}
		
		if val1 != val2 {
			return []DiffEntry{{
				Path:   path,
				Type:   DiffTypeValueMismatch,
				Value1: val1,
				Value2: val2,
			}}, nil
		}
		
	default:
		// Try to handle other types by converting to JSON and back
		jsonVal1, err := json.Marshal(val1)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal value 1: %w", err)
		}
		
		jsonVal2, err := json.Marshal(val2)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal value 2: %w", err)
		}
		
		if string(jsonVal1) != string(jsonVal2) {
			return []DiffEntry{{
				Path:   path,
				Type:   DiffTypeValueMismatch,
				Value1: string(jsonVal1),
				Value2: string(jsonVal2),
			}}, nil
		}
	}
	
	return nil, nil
}

// compareObjects compares two objects and returns their differences
func compareObjects(path string, obj1, obj2 map[string]interface{}) ([]DiffEntry, error) {
	var differences []DiffEntry
	
	// Get all keys from both objects
	allKeys := make(map[string]bool)
	for k := range obj1 {
		allKeys[k] = true
	}
	for k := range obj2 {
		allKeys[k] = true
	}
	
	// Sort keys for consistent output
	keys := make([]string, 0, len(allKeys))
	for k := range allKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	
	// Compare each key
	for _, key := range keys {
		keyPath := path
		if path != "" {
			keyPath = path + "." + key
		} else {
			keyPath = key
		}
		
		val1, exists1 := obj1[key]
		val2, exists2 := obj2[key]
		
		// Check if key exists in both objects
		if !exists1 {
			differences = append(differences, DiffEntry{
				Path:   keyPath,
				Type:   DiffTypeFieldMissing,
				Value1: nil,
				Value2: val2,
			})
			continue
		}
		
		if !exists2 {
			differences = append(differences, DiffEntry{
				Path:   keyPath,
				Type:   DiffTypeFieldExtra,
				Value1: val1,
				Value2: nil,
			})
			continue
		}
		
		// Recursively compare values
		diffs, err := deepCompare(keyPath, val1, val2)
		if err != nil {
			return nil, fmt.Errorf("failed to compare %s: %w", keyPath, err)
		}
		
		differences = append(differences, diffs...)
	}
	
	return differences, nil
}

// compareArrays compares two arrays and returns their differences
func compareArrays(path string, arr1, arr2 []interface{}) ([]DiffEntry, error) {
	var differences []DiffEntry
	
	// Compare array lengths
	if len(arr1) != len(arr2) {
		differences = append(differences, DiffEntry{
			Path:   path,
			Type:   DiffTypeArrayLengthMismatch,
			Value1: len(arr1),
			Value2: len(arr2),
		})
		
		// If lengths are different, we'll compare only up to the shorter length
		// to avoid index out of bounds errors
		minLen := len(arr1)
		if len(arr2) < minLen {
			minLen = len(arr2)
		}
		
		// Compare elements up to the minimum length
		for i := 0; i < minLen; i++ {
			itemPath := fmt.Sprintf("%s[%d]", path, i)
			diffs, err := deepCompare(itemPath, arr1[i], arr2[i])
			if err != nil {
				return nil, fmt.Errorf("failed to compare %s: %w", itemPath, err)
			}
			
			differences = append(differences, diffs...)
		}
		
		return differences, nil
	}
	
	// If lengths are the same, compare all elements
	for i := 0; i < len(arr1); i++ {
		itemPath := fmt.Sprintf("%s[%d]", path, i)
		diffs, err := deepCompare(itemPath, arr1[i], arr2[i])
		if err != nil {
			return nil, fmt.Errorf("failed to compare %s: %w", itemPath, err)
		}
		
		differences = append(differences, diffs...)
	}
	
	return differences, nil
}

// isZeroHex checks if a hex string contains only zeros after the 0x prefix
func isZeroHex(hexStr string) bool {
	// Remove 0x prefix
	if !strings.HasPrefix(hexStr, "0x") {
		return false
	}
	
	hexStr = hexStr[2:]
	
	// Check if the string is empty or contains only zeros
	for _, c := range hexStr {
		if c != '0' {
			return false
		}
	}
	
	return true
}

// FormatDifferences formats differences in a human-readable format
func FormatDifferences(diffs []DiffEntry) string {
	if len(diffs) == 0 {
		return "No differences found."
	}
	
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Found %d differences:\n", len(diffs)))
	
	for i, diff := range diffs {
		builder.WriteString(fmt.Sprintf("%d. Path: %s\n", i+1, diff.Path))
		builder.WriteString(fmt.Sprintf("   Type: %s\n", diff.Type))
		
		switch diff.Type {
		case DiffTypeValueMismatch:
			builder.WriteString(fmt.Sprintf("   Value 1: %v\n", diff.Value1))
			builder.WriteString(fmt.Sprintf("   Value 2: %v\n", diff.Value2))
			
		case DiffTypeTypeMismatch:
			builder.WriteString(fmt.Sprintf("   Type 1: %v\n", diff.Value1))
			builder.WriteString(fmt.Sprintf("   Type 2: %v\n", diff.Value2))
			
		case DiffTypeFieldMissing:
			builder.WriteString("   Field missing in first response\n")
			builder.WriteString(fmt.Sprintf("   Value in second: %v\n", diff.Value2))
			
		case DiffTypeFieldExtra:
			builder.WriteString("   Field extra in first response\n")
			builder.WriteString(fmt.Sprintf("   Value in first: %v\n", diff.Value1))
			
		case DiffTypeArrayLengthMismatch:
			builder.WriteString(fmt.Sprintf("   Length 1: %v\n", diff.Value1))
			builder.WriteString(fmt.Sprintf("   Length 2: %v\n", diff.Value2))
		}
		
		if diff.Reference != "" {
			builder.WriteString(fmt.Sprintf("   Reference: %s\n", diff.Reference))
		}
		
		builder.WriteString("\n")
	}
	
	return builder.String()
}
