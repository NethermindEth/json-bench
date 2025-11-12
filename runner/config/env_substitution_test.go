package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubstituteEnvVars_Basic(t *testing.T) {
	// Set up test environment variables
	os.Setenv("TEST_VAR", "test_value")
	os.Setenv("ANOTHER_VAR", "another_value")
	defer func() {
		os.Unsetenv("TEST_VAR")
		os.Unsetenv("ANOTHER_VAR")
	}()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple substitution",
			input:    "value: ${TEST_VAR}",
			expected: "value: test_value",
		},
		{
			name:     "multiple substitutions",
			input:    "first: ${TEST_VAR}, second: ${ANOTHER_VAR}",
			expected: "first: test_value, second: another_value",
		},
		{
			name:     "substitution in URL",
			input:    "url: http://${TEST_VAR}:8080",
			expected: "url: http://test_value:8080",
		},
		{
			name:     "no substitution",
			input:    "plain text without vars",
			expected: "plain text without vars",
		},
		{
			name:     "empty variable",
			input:    "value: ${EMPTY_VAR}",
			expected: "value: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := SubstituteEnvVars(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSubstituteEnvVars_DefaultValues(t *testing.T) {
	os.Setenv("SET_VAR", "set_value")
	defer os.Unsetenv("SET_VAR")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "use default when unset",
			input:    "value: ${UNSET_VAR:-default_value}",
			expected: "value: default_value",
		},
		{
			name:     "use actual value when set",
			input:    "value: ${SET_VAR:-default_value}",
			expected: "value: set_value",
		},
		{
			name:     "empty default",
			input:    "value: ${UNSET_VAR:-}",
			expected: "value: ",
		},
		{
			name:     "default with spaces",
			input:    "value: ${UNSET_VAR:-default with spaces}",
			expected: "value: default with spaces",
		},
		{
			name:     "default with special chars",
			input:    "value: ${UNSET_VAR:-http://localhost:8080}",
			expected: "value: http://localhost:8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := SubstituteEnvVars(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSubstituteEnvVars_RequiredVariables(t *testing.T) {
	os.Setenv("SET_VAR", "set_value")
	defer os.Unsetenv("SET_VAR")

	tests := []struct {
		name          string
		input         string
		expectedError string
		expectedValue string
	}{
		{
			name:          "error when required var unset",
			input:         "value: ${UNSET_VAR:?Variable is required}",
			expectedError: "Variable is required",
		},
		{
			name:          "error with default message",
			input:         "value: ${UNSET_VAR:?}",
			expectedError: "required environment variable UNSET_VAR is not set",
		},
		{
			name:          "no error when set",
			input:         "value: ${SET_VAR:?Variable is required}",
			expectedValue: "value: set_value",
		},
		{
			name:          "error message with spaces",
			input:         "value: ${UNSET_VAR:?This variable must be set}",
			expectedError: "This variable must be set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := SubstituteEnvVars(tt.input)
			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedValue, result)
			}
		})
	}
}

func TestSubstituteEnvVars_EscapeSequences(t *testing.T) {
	os.Setenv("TEST_VAR", "test_value")
	defer os.Unsetenv("TEST_VAR")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "escaped sequence",
			input:    "literal: $${TEST_VAR}",
			expected: "literal: ${TEST_VAR}",
		},
		{
			name:     "mixed escaped and unescaped",
			input:    "escaped: $${VAR}, substituted: ${TEST_VAR}",
			expected: "escaped: ${VAR}, substituted: test_value",
		},
		{
			name:     "multiple escaped",
			input:    "first: $${VAR1}, second: $${VAR2}",
			expected: "first: ${VAR1}, second: ${VAR2}",
		},
		{
			name:     "escaped with default",
			input:    "value: $${VAR:-default}",
			expected: "value: ${VAR:-default}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := SubstituteEnvVars(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSubstituteEnvVars_YAMLExamples(t *testing.T) {
	os.Setenv("HOST", "localhost")
	os.Setenv("PORT", "8545")
	os.Setenv("API_TOKEN", "secret_token")
	os.Setenv("INFURA_PROJECT_ID", "abc123")
	defer func() {
		os.Unsetenv("HOST")
		os.Unsetenv("PORT")
		os.Unsetenv("API_TOKEN")
		os.Unsetenv("INFURA_PROJECT_ID")
	}()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "YAML client config",
			input: `clients:
  - name: "geth"
    url: "http://${HOST}:${PORT}"
    auth:
      type: "bearer"
      token: "${API_TOKEN}"`,
			expected: `clients:
  - name: "geth"
    url: "http://localhost:8545"
    auth:
      type: "bearer"
      token: "secret_token"`,
		},
		{
			name: "YAML with default value",
			input: `clients:
  - name: "infura"
    url: "https://mainnet.infura.io/v3/${INFURA_PROJECT_ID}"
    timeout: "${TIMEOUT:-30s}"`,
			expected: `clients:
  - name: "infura"
    url: "https://mainnet.infura.io/v3/abc123"
    timeout: "30s"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := SubstituteEnvVars(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSubstituteEnvVars_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "unclosed brace",
			input:    "value: ${VAR",
			expected: "value: ${VAR",
		},
		{
			name:     "empty variable name",
			input:    "value: ${}",
			expected: "value: ",
		},
		{
			name:     "nested braces in default",
			input:    "value: ${VAR:-{nested}}",
			expected: "value: {nested}",
		},
		{
			name:     "variable name with spaces",
			input:    "value: ${VAR NAME}",
			expected: "value: ",
		},
		{
			name:     "multiple dollar signs",
			input:    "value: $$${VAR}",
			expected: "value: $${VAR}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := SubstituteEnvVars(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

