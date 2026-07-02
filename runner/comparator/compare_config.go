package comparator

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"

	"github.com/jsonrpc-bench/runner/config"
)

// compareCall is a single {method, id?, params} entry from a compare config.
type compareCall struct {
	ID     string        `yaml:"id"`
	Params []interface{} `yaml:"params"`
}

// compareFile is the on-disk shape of a compare YAML config.
type compareFile struct {
	Name        string                   `yaml:"name"`
	Description string                   `yaml:"description"`
	Calls       yaml.Node                `yaml:"calls"`
	// Populated from Calls after decoding; preserves insertion order.
	calls []methodCalls `yaml:"-"`
}

// methodCalls preserves the order of methods as they appear in the YAML.
type methodCalls struct {
	Method string
	Calls  []compareCall
}

// idPattern restricts user-supplied ids to a filesystem/URL-safe subset.
var idPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// reservedIDPattern matches the "variantN" suffix reserved for auto-generated
// identifiers so a user-supplied id cannot collide with them.
var reservedIDPattern = regexp.MustCompile(`^variant\d+$`)

// LoadCompareConfig loads a YAML config describing the JSON-RPC calls that
// `runner compare` should send. It fills the Name, Description, Methods,
// MethodRPCNames and CustomParameters fields; every other field on the
// returned *ComparisonConfig is left zero for the caller to populate from
// CLI flags.
func LoadCompareConfig(path string) (*ComparisonConfig, error) {
	safePath, err := config.SafeReadPath(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(safePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read compare config: %w", err)
	}

	var file compareFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("failed to parse compare config: %w", err)
	}
	if err := decodeCallsMap(&file.Calls, &file.calls); err != nil {
		return nil, err
	}

	if file.Name == "" {
		return nil, fmt.Errorf("compare config: 'name' is required")
	}
	if len(file.calls) == 0 {
		return nil, fmt.Errorf("compare config: 'calls' must contain at least one method")
	}

	cfg := &ComparisonConfig{
		Name:             file.Name,
		Description:      file.Description,
		Methods:          make([]string, 0),
		MethodRPCNames:   make(map[string]string),
		CustomParameters: make(map[string][]interface{}),
	}

	for _, m := range file.calls {
		if len(m.Calls) == 0 {
			return nil, fmt.Errorf("compare config: method %q has no calls", m.Method)
		}
		seenIDs := make(map[string]struct{}, len(m.Calls))
		for i, call := range m.Calls {
			if call.Params == nil {
				return nil, fmt.Errorf("compare config: method %q entry %d is missing required 'params' (use 'params: []' for none)", m.Method, i+1)
			}
			var suffix string
			if call.ID != "" {
				if !idPattern.MatchString(call.ID) {
					return nil, fmt.Errorf("compare config: method %q entry %d: id %q must match [a-zA-Z0-9_-]+", m.Method, i+1, call.ID)
				}
				if reservedIDPattern.MatchString(call.ID) {
					return nil, fmt.Errorf("compare config: method %q entry %d: id %q collides with the reserved 'variant<N>' form", m.Method, i+1, call.ID)
				}
				if _, dup := seenIDs[call.ID]; dup {
					return nil, fmt.Errorf("compare config: method %q entry %d: id %q is duplicated within this method", m.Method, i+1, call.ID)
				}
				seenIDs[call.ID] = struct{}{}
				suffix = call.ID
			} else {
				suffix = fmt.Sprintf("variant%d", i+1)
			}
			identifier := fmt.Sprintf("%s_%s", m.Method, suffix)
			cfg.Methods = append(cfg.Methods, identifier)
			cfg.MethodRPCNames[identifier] = m.Method
			cfg.CustomParameters[identifier] = call.Params
		}
	}

	return cfg, nil
}

// decodeCallsMap walks a `calls:` yaml.Node preserving map insertion order.
// The stock yaml.v3 map decoder loses ordering, which we care about because
// the identifiers we emit are 1-indexed by position within a method.
func decodeCallsMap(node *yaml.Node, out *[]methodCalls) error {
	if node == nil || node.Kind == 0 {
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("compare config: 'calls' must be a mapping of method-name to list of calls")
	}
	if len(node.Content)%2 != 0 {
		return fmt.Errorf("compare config: malformed 'calls' mapping")
	}
	seenMethods := make(map[string]struct{}, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valNode := node.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode {
			return fmt.Errorf("compare config: method names must be scalars, got %v", keyNode.Tag)
		}
		method := keyNode.Value
		if method == "" {
			return fmt.Errorf("compare config: method name cannot be empty")
		}
		if _, dup := seenMethods[method]; dup {
			return fmt.Errorf("compare config: method %q is listed more than once", method)
		}
		seenMethods[method] = struct{}{}

		var calls []compareCall
		if err := valNode.Decode(&calls); err != nil {
			return fmt.Errorf("compare config: method %q: %w", method, err)
		}
		*out = append(*out, methodCalls{Method: method, Calls: calls})
	}
	return nil
}
