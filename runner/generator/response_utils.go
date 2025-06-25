package generator

import (
	"github.com/jsonrpc-bench/runner/comparator"
	"github.com/jsonrpc-bench/runner/types"
)

// ConvertToResponseDiff converts a ComparisonResult to a ResponseDiff
// with all the fields needed for the HTML report
func ConvertToResponseDiff(result comparator.ComparisonResult) types.ResponseDiff {
	// Extract client names from responses
	clientNames := make([]string, 0, len(result.Responses))
	for client := range result.Responses {
		clientNames = append(clientNames, client)
	}

	// Check if there are differences
	hasDiff := len(result.Differences) > 0

	// Create ResponseDiff with all fields populated
	return types.ResponseDiff{
		Method:       result.Method,
		Params:       result.Params,
		Clients:      clientNames,
		ClientNames:  clientNames,
		Responses:    result.Responses,
		Differences:  result.Differences,
		SchemaErrors: result.SchemaErrors,
		HasDiff:      hasDiff,
	}
}

// ConvertComparisonResults converts a slice of ComparisonResult to a slice of ResponseDiff
func ConvertComparisonResults(results []comparator.ComparisonResult) []types.ResponseDiff {
	diffs := make([]types.ResponseDiff, len(results))
	for i, result := range results {
		diffs[i] = ConvertToResponseDiff(result)
	}
	return diffs
}
