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
