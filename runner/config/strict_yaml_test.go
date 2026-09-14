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

func TestLoadShapeValidation(t *testing.T) {
	base := `
test_name: "shape"
clients: ["geth"]
vus: 10
calls:
  - name: "eth_call"
    method: "eth_call"
    params: []
    weight: 1
`
	cases := map[string]struct {
		extra string
		error string
	}{
		"a rate for a duration":        {extra: "duration: \"1m\"\nrps: 100\n"},
		"a fixed number of iterations": {extra: "duration: \"1m\"\niterations: 500\n"},
		"a ramp through stages":        {extra: "rps: 10\nstages:\n  - duration: \"30s\"\n    target: 100\n"},
		"a ramp with a warmup":         {extra: "rps: 10\nwarmup: \"5s\"\nstages:\n  - duration: \"30s\"\n    target: 100\n"},

		// The run's length must be stated once.
		"stages and duration together": {
			extra: "duration: \"1m\"\nrps: 10\nstages:\n  - duration: \"30s\"\n    target: 100\n",
			error: "duration must be omitted",
		},
		"stages and iterations together": {
			extra: "iterations: 100\nstages:\n  - duration: \"30s\"\n    target: 100\n",
			error: "cannot be combined with iterations",
		},
		// Warmup discards a period, which needs the load paced by time.
		"warmup with iterations": {
			extra: "duration: \"1m\"\niterations: 100\nwarmup: \"5s\"\n",
			error: "cannot be combined with iterations",
		},
		"negative stage target": {
			extra: "rps: 10\nstages:\n  - duration: \"30s\"\n    target: -1\n",
			error: "negative target",
		},
		"zero-length stage": {
			extra: "rps: 10\nstages:\n  - duration: \"0s\"\n    target: 10\n",
			error: "positive duration",
		},
		"unparseable stage duration": {
			extra: "rps: 10\nstages:\n  - duration: \"soon\"\n    target: 10\n",
			error: "invalid duration",
		},
		"unparseable warmup": {
			extra: "duration: \"1m\"\nrps: 10\nwarmup: \"soon\"\n",
			error: "invalid warmup",
		},
		"neither rate nor iterations": {
			extra: "duration: \"1m\"\n",
			error: "either iterations or rps",
		},
		"no length at all": {
			extra: "rps: 100\n",
			error: "duration is required",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			path := writeTemp(t, "config.yaml", base+tc.extra)
			_, err := NewConfigLoader(registryWith(t, "geth")).LoadTestConfig(path)
			if tc.error == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.error)
		})
	}
}

// Iterations counts requests and batching groups them, so a single number
// would be ambiguous about which it meant.
func TestBatchSizeValidation(t *testing.T) {
	base := `
test_name: "batch"
clients: ["geth"]
vus: 10
calls:
  - name: "eth_call"
    method: "eth_call"
    params: []
    weight: 1
`
	cases := map[string]string{
		"duration: \"1m\"\nrps: 100\nbatch_size: 10\n":       "",
		"duration: \"1m\"\nrps: 100\nbatch_size: 1\n":        "",
		"duration: \"1m\"\nrps: 100\nbatch_size: -1\n":       "cannot be negative",
		"duration: \"1m\"\niterations: 100\nbatch_size: 5\n": "cannot be combined with iterations",
	}

	for extra, wantErr := range cases {
		t.Run(extra, func(t *testing.T) {
			path := writeTemp(t, "config.yaml", base+extra)
			_, err := NewConfigLoader(registryWith(t, "geth")).LoadTestConfig(path)
			if wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), wantErr)
		})
	}
}

// A negative weight does not merely silence its own call: it shrinks the total
// the weighted draw divides by, so every other call's share is wrong too.
func TestLoadTestConfigRejectsANegativeWeight(t *testing.T) {
	path := writeTemp(t, "config.yaml", `
test_name: "weights"
clients: ["geth"]
duration: "1m"
rps: 100
vus: 10
calls:
  - name: "eth_call"
    method: "eth_call"
    params: []
    weight: -1
  - name: "eth_getLogs"
    method: "eth_getLogs"
    params: []
    weight: 2
`)

	_, err := NewConfigLoader(registryWith(t, "geth")).LoadTestConfig(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "negative weight")
}
