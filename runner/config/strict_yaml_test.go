package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jsonrpc-bench/runner/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTemp(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func registryWith(t *testing.T, names ...string) *ClientRegistry {
	t.Helper()
	clients := make([]types.ClientConfig, 0, len(names))
	for _, name := range names {
		clients = append(clients, types.ClientConfig{Name: name, URL: "http://127.0.0.1:8545"})
	}
	registry := NewClientRegistry()
	require.NoError(t, registry.LoadFromConfig(types.ClientsConfig{Clients: clients}))
	return registry
}

// `frequency` was accepted and ignored by committed profiles, which gave the
// call it was written on zero traffic instead of the intended share.
func TestLoadTestConfigRejectsFrequency(t *testing.T) {
	path := writeTemp(t, "config.yaml", `
test_name: "legacy"
clients: ["geth"]
duration: "1m"
rps: 100
vus: 10
calls:
  - name: "eth_call"
    method: "eth_call"
    params: []
    frequency: 10%
`)

	_, err := NewConfigLoader(registryWith(t, "geth")).LoadTestConfig(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "frequency")
}

func TestLoadTestConfigRejectsUnknownTopLevelKey(t *testing.T) {
	path := writeTemp(t, "config.yaml", `
test_name: "typo"
clients: ["geth"]
duration: "1m"
rps: 100
vus: 10
warmpup: "30s"
calls:
  - name: "eth_call"
    method: "eth_call"
    params: []
    weight: 1
`)

	_, err := NewConfigLoader(registryWith(t, "geth")).LoadTestConfig(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "warmpup")
}

func TestLoadTestConfigAcceptsEveryDocumentedKey(t *testing.T) {
	path := writeTemp(t, "config.yaml", `
test_name: "complete"
description: "exercises the whole schema"
clients: ["geth"]
duration: "1m"
rps: 100
vus: 10
seed: 42
calls:
  - name: "eth_call"
    method: "eth_call"
    params: []
    weight: 1
    thresholds: ["p(99)<600000"]
`)

	cfg, err := NewConfigLoader(registryWith(t, "geth")).LoadTestConfig(path)
	require.NoError(t, err)
	assert.Equal(t, "complete", cfg.TestName)
	assert.EqualValues(t, 42, cfg.Seed)
	assert.Equal(t, []string{"p(99)<600000"}, cfg.Calls[0].Thresholds)
}

func TestClientRegistryRejectsUnknownKey(t *testing.T) {
	path := writeTemp(t, "clients.yaml", `
clients:
  - name: geth
    url: "http://127.0.0.1:8545"
    timeuot: "30s"
`)

	err := NewClientRegistry().LoadFromFile(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeuot")
}

// Old-style configs embed client definitions instead of referencing them. The
// iterations load mode was missing from that struct, so it decoded to zero and
// the run failed validation as if neither rps nor iterations had been set.
func TestOldStyleConfigKeepsIterations(t *testing.T) {
	path := writeTemp(t, "config.yaml", `
test_name: "embedded"
clients:
  - name: geth
    url: "http://127.0.0.1:8545"
duration: "1m"
iterations: 500
vus: 10
calls:
  - name: "eth_call"
    method: "eth_call"
    params: []
    weight: 1
`)

	cfg, err := NewConfigLoader(NewClientRegistry()).LoadWithBackwardCompatibility(path)
	require.NoError(t, err)
	assert.Equal(t, 500, cfg.Iterations)
	assert.Equal(t, 0, cfg.RPS)
}

func TestUnmarshalStrictTreatsEmptyDocumentAsZeroValue(t *testing.T) {
	var cfg Config
	require.NoError(t, UnmarshalStrict(nil, &cfg))
	require.NoError(t, UnmarshalStrict([]byte("# only a comment\n"), &cfg))
	assert.Empty(t, cfg.TestName)
}
