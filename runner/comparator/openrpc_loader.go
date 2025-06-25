package comparator

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// OpenRPCSpec represents the structure of an OpenRPC specification
type OpenRPCSpec struct {
	Info struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Version     string `json:"version"`
	} `json:"info"`
	Methods []OpenRPCMethod `json:"methods"`
}

// OpenRPCMethod represents a method in the OpenRPC specification
type OpenRPCMethod struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Params      []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Schema      struct {
			Type    string `json:"type"`
			Default interface{} `json:"default,omitempty"`
		} `json:"schema"`
		Required bool `json:"required"`
	} `json:"params"`
}

// Method represents a method with parameters
type Method struct {
	Name   string
	Params []interface{}
}

// LoadMethodsFromOpenRPC loads methods from an OpenRPC specification with optional parameter variations
func LoadMethodsFromOpenRPC(specPath string, variationsPath string) (*ComparisonConfig, error) {
	// Load OpenRPC spec
	spec, err := loadOpenRPCSpec(specPath)
	if err != nil {
		return nil, err
	}

	// Load parameter variations if provided
	var variations ParamVariations
	if variationsPath != "" {
		variations, err = LoadParamVariations(variationsPath)
		if err != nil {
			fmt.Printf("Warning: Failed to load parameter variations: %v\n", err)
			// Continue without variations
			variations = make(ParamVariations)
		}
	} else {
		variations = make(ParamVariations)
	}

	// Create comparison config
	config := &ComparisonConfig{
		Name:                 spec.Info.Title,
		Description:          spec.Info.Description,
		Methods:              make([]string, 0),
		CustomParameters:     make(map[string][]interface{}),
		ValidateAgainstSchema: true,
		Concurrency:          5,
		TimeoutSeconds:       30,
		OutputDir:            "comparison-results",
	}

	// Process methods and their parameters
	processedMethods := make(map[string]bool)
	for _, method := range spec.Methods {
		// Skip if we've already processed this method
		if processedMethods[method.Name] {
			continue
		}

		// Add method to the list
		config.Methods = append(config.Methods, method.Name)
		processedMethods[method.Name] = true

		// Check if we have parameter variations for this method
		methodVariations := variations.GetVariations(method.Name)
		if len(methodVariations) > 0 {
			// Use the provided variations
			for i, params := range methodVariations {
				variantName := fmt.Sprintf("%s_variant%d", method.Name, i+1)
				config.Methods = append(config.Methods, variantName)
				config.CustomParameters[variantName] = params
			}
		} else {
			// Generate default parameters
			if len(method.Params) > 0 {
				params := make([]interface{}, 0, len(method.Params))
				for _, param := range method.Params {
					// Use default value if available, otherwise use a sensible default based on type
					if param.Schema.Default != nil {
						params = append(params, param.Schema.Default)
					} else {
						switch param.Schema.Type {
						case "string":
							params = append(params, "")
						case "number", "integer":
							params = append(params, 0)
						case "boolean":
							params = append(params, false)
						case "array":
							params = append(params, []interface{}{})
						case "object":
							params = append(params, map[string]interface{}{})
						default:
							params = append(params, nil)
						}
					}
				}
				config.CustomParameters[method.Name] = params
			}
		}
	}

	return config, nil
}

func loadOpenRPCSpec(specPath string) (*OpenRPCSpec, error) {
	var data []byte
	var err error

	// Check if specPath is a URL or a file path
	if isURL(specPath) {
		// Download from URL
		client := http.Client{
			Timeout: 30 * time.Second,
		}
		resp, err := client.Get(specPath)
		if err != nil {
			return nil, fmt.Errorf("failed to download OpenRPC spec: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("failed to download OpenRPC spec: HTTP %d", resp.StatusCode)
		}

		data, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read OpenRPC spec: %w", err)
		}
	} else {
		// Read from file
		data, err = os.ReadFile(specPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read OpenRPC spec file: %w", err)
		}
	}

	// Parse OpenRPC spec
	var spec OpenRPCSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("failed to parse OpenRPC spec: %w", err)
	}

	return &spec, nil
}

// isURL checks if a string is a URL
func isURL(s string) bool {
	return len(s) >= 7 && (s[:7] == "http://" || s[:8] == "https://")
}
