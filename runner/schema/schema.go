package schema

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/xeipuuv/gojsonschema"
)

const (
	// EthereumAPIBaseURL is the base URL for the Ethereum execution API schemas
	EthereumAPIBaseURL = "https://raw.githubusercontent.com/ethereum/execution-apis/main/openrpc.json"

	// LocalSchemaDir is the directory where schemas are cached
	LocalSchemaDir = "schemas"
)

var (
	// Cache for loaded schemas
	schemaCache     = make(map[string]*gojsonschema.Schema)
	schemaCacheLock sync.RWMutex
)

// SchemaValidator handles validation of JSON-RPC responses against Ethereum execution API schemas
type SchemaValidator struct {
	openRPCSpec   map[string]interface{}
	methodSchemas map[string]*gojsonschema.Schema
}

// NewSchemaValidator creates a new schema validator
func NewSchemaValidator() (*SchemaValidator, error) {
	// Create schema directory if it doesn't exist
	if err := os.MkdirAll(LocalSchemaDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create schema directory: %w", err)
	}

	// Load or download the OpenRPC spec
	spec, err := loadOpenRPCSpec()
	if err != nil {
		return nil, fmt.Errorf("failed to load OpenRPC spec: %w", err)
	}

	// Extract method schemas
	methodSchemas, err := extractMethodSchemas(spec)
	if err != nil {
		return nil, fmt.Errorf("failed to extract method schemas: %w", err)
	}

	return &SchemaValidator{
		openRPCSpec:   spec,
		methodSchemas: methodSchemas,
	}, nil
}

// ValidateResponse validates a JSON-RPC response against the Ethereum execution API schema
func (v *SchemaValidator) ValidateResponse(method string, response map[string]interface{}) (bool, []string, error) {
	// Get the schema for this method
	schema, ok := v.methodSchemas[method]
	if !ok {
		return false, []string{fmt.Sprintf("no schema found for method %s", method)}, nil
	}

	// Extract the result from the response
	result, ok := response["result"]
	if !ok {
		// Check if there's an error
		if _, hasError := response["error"]; hasError {
			// Errors are valid responses in JSON-RPC
			return true, nil, nil
		}
		return false, []string{"response missing both result and error fields"}, nil
	}

	// Convert result to JSON
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return false, nil, fmt.Errorf("failed to marshal result to JSON: %w", err)
	}

	// Validate against schema
	resultLoader := gojsonschema.NewStringLoader(string(resultJSON))
	validationResult, err := schema.Validate(resultLoader)
	if err != nil {
		return false, nil, fmt.Errorf("validation error: %w", err)
	}

	// Check validation result
	if !validationResult.Valid() {
		// Collect validation errors
		errors := make([]string, len(validationResult.Errors()))
		for i, err := range validationResult.Errors() {
			errors[i] = err.String()
		}
		return false, errors, nil
	}

	return true, nil, nil
}

// GetSupportedMethods returns a list of methods that have schemas
func (v *SchemaValidator) GetSupportedMethods() []string {
	methods := make([]string, 0, len(v.methodSchemas))
	for method := range v.methodSchemas {
		methods = append(methods, method)
	}
	return methods
}

// loadOpenRPCSpec loads the Ethereum execution API OpenRPC spec
func loadOpenRPCSpec() (map[string]interface{}, error) {
	specPath := filepath.Join(LocalSchemaDir, "openrpc.json")

	// Check if we already have the spec locally
	if _, err := os.Stat(specPath); err == nil {
		// Load from local file
		data, err := ioutil.ReadFile(specPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read local schema file: %w", err)
		}

		var spec map[string]interface{}
		if err := json.Unmarshal(data, &spec); err != nil {
			return nil, fmt.Errorf("failed to parse local schema file: %w", err)
		}

		return spec, nil
	}

	// Download the spec
	resp, err := http.Get(EthereumAPIBaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to download schema: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download schema: HTTP %d", resp.StatusCode)
	}

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read schema response: %w", err)
	}

	// Save to local file
	if err := ioutil.WriteFile(specPath, data, 0644); err != nil {
		return nil, fmt.Errorf("failed to write schema to local file: %w", err)
	}

	// Parse the spec
	var spec map[string]interface{}
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("failed to parse schema: %w", err)
	}

	return spec, nil
}

// extractMethodSchemas extracts JSON schemas for each method's result from the OpenRPC spec
func extractMethodSchemas(spec map[string]interface{}) (map[string]*gojsonschema.Schema, error) {
	methodSchemas := make(map[string]*gojsonschema.Schema)

	// Extract methods from the spec
	methods, ok := spec["methods"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid OpenRPC spec: methods not found or invalid format")
	}

	for _, methodObj := range methods {
		method, ok := methodObj.(map[string]interface{})
		if !ok {
			continue
		}

		// Get method name
		methodName, ok := method["name"].(string)
		if !ok {
			continue
		}

		// Get result schema
		result, ok := method["result"].(map[string]interface{})
		if !ok {
			continue
		}

		schema, ok := result["schema"].(map[string]interface{})
		if !ok {
			continue
		}

		// Convert schema to JSON
		schemaJSON, err := json.Marshal(schema)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal schema for method %s: %w", methodName, err)
		}

		// Compile schema
		schemaLoader := gojsonschema.NewStringLoader(string(schemaJSON))
		compiledSchema, err := gojsonschema.NewSchema(schemaLoader)
		if err != nil {
			return nil, fmt.Errorf("failed to compile schema for method %s: %w", methodName, err)
		}

		methodSchemas[methodName] = compiledSchema
	}

	return methodSchemas, nil
}

// ValidateResponseAgainstSchema validates a JSON-RPC response against its schema
func ValidateResponseAgainstSchema(method string, response map[string]interface{}) (bool, []string, error) {
	validator, err := NewSchemaValidator()
	if err != nil {
		return false, nil, fmt.Errorf("failed to create schema validator: %w", err)
	}

	return validator.ValidateResponse(method, response)
}
