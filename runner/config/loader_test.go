package config

import (
	"os"
	"testing"

	"github.com/jsonrpc-bench/runner/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigLoader(t *testing.T) {
	// Create a test client registry
	registry := NewClientRegistry()

	// Load some test clients
	testClients := types.ClientsConfig{
		Clients: []types.ClientConfig{
			{
				Name: "geth",
				URL:  "http://localhost:8545",
			},
			{
				Name: "erigon",
				URL:  "http://localhost:8546",
			},
		},
	}

	err := registry.LoadFromConfig(testClients)
	require.NoError(t, err)

	// Create config loader
	loader := NewConfigLoader(registry)

	t.Run("LoadTestConfig", func(t *testing.T) {
		// Create a test config file
		testConfig := `
test_name: "test-benchmark"
description: "Test benchmark configuration"
clients:
  - geth
  - erigon
duration: "30s"
rps: 100
endpoints:
  - method: "eth_blockNumber"
    params: []
    frequency: "50%"
  - method: "eth_chainId"
    params: []
    frequency: "50%"
validate_responses: true
`
		tmpFile, err := os.CreateTemp("", "test-config-*.yaml")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())

		_, err = tmpFile.Write([]byte(testConfig))
		require.NoError(t, err)
		tmpFile.Close()

		// Load the config
		config, err := loader.LoadTestConfig(tmpFile.Name())
		require.NoError(t, err)

		// Verify the config
		assert.Equal(t, "test-benchmark", config.TestName)
		assert.Equal(t, "Test benchmark configuration", config.Description)
		assert.Equal(t, []string{"geth", "erigon"}, config.ClientRefs)
		assert.Len(t, config.ResolvedClients, 2)
		assert.Equal(t, "geth", config.ResolvedClients[0].Name)
		assert.Equal(t, "erigon", config.ResolvedClients[1].Name)
	})

	t.Run("ResolveClientReferences", func(t *testing.T) {
		config := &Config{
			ClientRefs: []string{"geth", "erigon"},
		}

		err := loader.ResolveClientReferences(config)
		require.NoError(t, err)

		assert.Len(t, config.ResolvedClients, 2)
		assert.Equal(t, "geth", config.ResolvedClients[0].Name)
		assert.Equal(t, "http://localhost:8545", config.ResolvedClients[0].URL)
		assert.Equal(t, "erigon", config.ResolvedClients[1].Name)
		assert.Equal(t, "http://localhost:8546", config.ResolvedClients[1].URL)
	})

	t.Run("ResolveClientReferences_NotFound", func(t *testing.T) {
		config := &Config{
			ClientRefs: []string{"nonexistent"},
		}

		err := loader.ResolveClientReferences(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "client not found in registry: nonexistent")
	})

	t.Run("LoadWithBackwardCompatibility_NewStyle", func(t *testing.T) {
		// Test new style config with client references
		testConfig := `
test_name: "new-style-test"
description: "New style configuration"
clients:
  - geth
  - erigon
duration: "30s"
rps: 100
endpoints:
  - method: "eth_blockNumber"
    params: []
    frequency: "100%"
validate_responses: true
`
		tmpFile, err := os.CreateTemp("", "test-new-style-*.yaml")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())

		_, err = tmpFile.Write([]byte(testConfig))
		require.NoError(t, err)
		tmpFile.Close()

		config, err := loader.LoadWithBackwardCompatibility(tmpFile.Name())
		require.NoError(t, err)

		assert.Equal(t, "new-style-test", config.TestName)
		assert.Equal(t, []string{"geth", "erigon"}, config.ClientRefs)
		assert.Len(t, config.ResolvedClients, 2)
	})

	t.Run("LoadWithBackwardCompatibility_OldStyle", func(t *testing.T) {
		// Test old style config with embedded clients
		testConfig := `
test_name: "old-style-test"
description: "Old style configuration"
clients:
  - name: "local-geth"
    url: "http://localhost:9545"
  - name: "local-erigon"
    url: "http://localhost:9546"
duration: "30s"
rps: 100
endpoints:
  - method: "eth_blockNumber"
    params: []
    frequency: "100%"
validate_responses: true
`
		tmpFile, err := os.CreateTemp("", "test-old-style-*.yaml")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())

		_, err = tmpFile.Write([]byte(testConfig))
		require.NoError(t, err)
		tmpFile.Close()

		config, err := loader.LoadWithBackwardCompatibility(tmpFile.Name())
		require.NoError(t, err)

		assert.Equal(t, "old-style-test", config.TestName)
		assert.Equal(t, []string{"local-geth", "local-erigon"}, config.ClientRefs)
		assert.Len(t, config.ResolvedClients, 2)
		assert.Equal(t, "local-geth", config.ResolvedClients[0].Name)
		assert.Equal(t, "http://localhost:9545", config.ResolvedClients[0].URL)
	})
}

func TestConfigLoader_EnvVarSubstitution(t *testing.T) {
	// Set up test environment variables
	os.Setenv("TEST_HOST", "testhost")
	os.Setenv("TEST_PORT", "8545")
	os.Setenv("TEST_DURATION", "60s")
	os.Setenv("TEST_RPS", "200")
	os.Setenv("TEST_VUS", "1")
	defer func() {
		os.Unsetenv("TEST_HOST")
		os.Unsetenv("TEST_PORT")
		os.Unsetenv("TEST_DURATION")
		os.Unsetenv("TEST_RPS")
		os.Unsetenv("TEST_VUS")
	}()

	// Create a test client registry
	registry := NewClientRegistry()
	testClients := types.ClientsConfig{
		Clients: []types.ClientConfig{
			{
				Name: "geth",
				URL:  "http://localhost:8545",
			},
		},
	}
	err := registry.LoadFromConfig(testClients)
	require.NoError(t, err)

	loader := NewConfigLoader(registry)

	t.Run("LoadTestConfig_WithEnvVars", func(t *testing.T) {
		testConfig := `
test_name: "env-test"
description: "Test with ${TEST_HOST}:${TEST_PORT}"
clients:
  - geth
duration: "${TEST_DURATION}"
rps: ${TEST_RPS}
vus: 1
calls:
  - name: "blockNumber"
    method: "eth_blockNumber"
    params: []
validate_responses: true
`
		tmpFile, err := os.CreateTemp("", "test-env-config-*.yaml")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())

		_, err = tmpFile.Write([]byte(testConfig))
		require.NoError(t, err)
		tmpFile.Close()

		config, err := loader.LoadTestConfig(tmpFile.Name())
		require.NoError(t, err)

		assert.Equal(t, "env-test", config.TestName)
		assert.Equal(t, "Test with testhost:8545", config.Description)
		assert.Equal(t, "60s", config.Duration)
		assert.Equal(t, 200, config.RPS)
	})

	t.Run("LoadTestConfig_WithDefaultValue", func(t *testing.T) {
		testConfig := `
test_name: "env-default-test"
description: "Test with default"
clients:
  - geth
duration: "${UNSET_VAR:-30s}"
rps: 100
vus: 1
calls:
  - name: "blockNumber"
    method: "eth_blockNumber"
    params: []
validate_responses: true
`
		tmpFile, err := os.CreateTemp("", "test-env-default-*.yaml")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())

		_, err = tmpFile.Write([]byte(testConfig))
		require.NoError(t, err)
		tmpFile.Close()

		config, err := loader.LoadTestConfig(tmpFile.Name())
		require.NoError(t, err)

		assert.Equal(t, "30s", config.Duration)
	})

	t.Run("LoadTestConfig_WithRequiredVar_Error", func(t *testing.T) {
		testConfig := `
test_name: "env-required-test"
description: "Test with required var"
clients:
  - geth
duration: "${REQUIRED_VAR:?This variable is required}"
rps: 100
vus: 1
calls:
  - name: "blockNumber"
    method: "eth_blockNumber"
    params: []
validate_responses: true
`
		tmpFile, err := os.CreateTemp("", "test-env-required-*.yaml")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())

		_, err = tmpFile.Write([]byte(testConfig))
		require.NoError(t, err)
		tmpFile.Close()

		_, err = loader.LoadTestConfig(tmpFile.Name())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "This variable is required")
	})

	t.Run("LoadWithBackwardCompatibility_WithEnvVars", func(t *testing.T) {
		testConfig := `
test_name: "env-backward-test"
description: "Test backward compatibility with env vars"
clients:
  - name: "env_geth"
    url: "http://${TEST_HOST}:${TEST_PORT}"
duration: "${TEST_DURATION}"
rps: ${TEST_RPS}
vus: ${TEST_VUS:-1}
calls:
  - name: "blockNumber"
    method: "eth_blockNumber"
    params: []
validate_responses: true
`
		tmpFile, err := os.CreateTemp("", "test-env-backward-*.yaml")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())

		_, err = tmpFile.Write([]byte(testConfig))
		require.NoError(t, err)
		tmpFile.Close()

		config, err := loader.LoadWithBackwardCompatibility(tmpFile.Name())
		require.NoError(t, err)

		assert.Equal(t, "env-backward-test", config.TestName)
		assert.Equal(t, "60s", config.Duration)
		assert.Equal(t, 200, config.RPS)
		assert.Len(t, config.ResolvedClients, 1)
		assert.Equal(t, "env_geth", config.ResolvedClients[0].Name)
		assert.Equal(t, "http://testhost:8545", config.ResolvedClients[0].URL)
	})
}

func TestClientRegistry_EnvVarSubstitution(t *testing.T) {
	// Set up test environment variables
	os.Setenv("CLIENT_HOST", "testhost")
	os.Setenv("CLIENT_PORT", "8545")
	os.Setenv("CLIENT_TIMEOUT", "60s")
	os.Setenv("API_TOKEN", "test_token")
	defer func() {
		os.Unsetenv("CLIENT_HOST")
		os.Unsetenv("CLIENT_PORT")
		os.Unsetenv("CLIENT_TIMEOUT")
		os.Unsetenv("API_TOKEN")
	}()

	registry := NewClientRegistry()

	t.Run("LoadFromFile_WithEnvVars", func(t *testing.T) {
		clientConfig := `
clients:
  - name: "env_client"
    type: "geth"
    url: "http://${CLIENT_HOST}:${CLIENT_PORT}"
    timeout: "${CLIENT_TIMEOUT}"
    auth:
      type: "bearer"
      token: "${API_TOKEN}"
`
		tmpFile, err := os.CreateTemp("", "test-clients-env-*.yaml")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())

		_, err = tmpFile.Write([]byte(clientConfig))
		require.NoError(t, err)
		tmpFile.Close()

		err = registry.LoadFromFile(tmpFile.Name())
		require.NoError(t, err)

		client, exists := registry.Get("env_client")
		require.True(t, exists)
		assert.Equal(t, "http://testhost:8545", client.URL)
		assert.Equal(t, "60s", client.Timeout)
		assert.NotNil(t, client.Auth)
		assert.Equal(t, "bearer", client.Auth.Type)
		assert.Equal(t, "test_token", client.Auth.Token)
	})

	t.Run("LoadFromFile_WithDefaultValue", func(t *testing.T) {
		clientConfig := `
clients:
  - name: "default_client"
    type: "geth"
    url: "http://${CLIENT_HOST}:${CLIENT_PORT}"
    timeout: "${UNSET_TIMEOUT:-30s}"
`
		tmpFile, err := os.CreateTemp("", "test-clients-default-*.yaml")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())

		_, err = tmpFile.Write([]byte(clientConfig))
		require.NoError(t, err)
		tmpFile.Close()

		err = registry.LoadFromFile(tmpFile.Name())
		require.NoError(t, err)

		client, exists := registry.Get("default_client")
		require.True(t, exists)
		assert.Equal(t, "30s", client.Timeout)
	})
}
