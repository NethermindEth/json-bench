package comparator

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ParamVariations represents parameter variations for methods
type ParamVariations map[string][][]interface{}

// LoadParamVariations loads parameter variations from a YAML file
func LoadParamVariations(filePath string) (ParamVariations, error) {
	// Read the YAML file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read parameter variations file: %w", err)
	}

	// Parse the YAML data
	variations := make(ParamVariations)
	if err := yaml.Unmarshal(data, &variations); err != nil {
		return nil, fmt.Errorf("failed to parse parameter variations: %w", err)
	}

	return variations, nil
}

// GetVariations returns parameter variations for a method
func (p ParamVariations) GetVariations(methodName string) [][]interface{} {
	if variations, exists := p[methodName]; exists {
		return variations
	}
	return nil
}
