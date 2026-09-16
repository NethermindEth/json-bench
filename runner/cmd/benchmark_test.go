package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/types"
)

func batchedConfig() *config.Config {
	return &config.Config{
		TestName:        "batching",
		Duration:        "30s",
		RPS:             100,
		VUs:             10,
		BatchSize:       10,
		Calls:           []*config.Call{{Name: "call", Method: "eth_call", Params: []any{}, Weight: 1}},
		ClientRefs:      []string{"stub"},
		ResolvedClients: []*types.ClientConfig{{Name: "stub", URL: "http://127.0.0.1:8545"}},
	}
}

// --batch-size acts on having been given, not on its value. Reading the value
// instead made `--batch-size 0` — the obvious way to turn batching off for one
// run of a config that sets it — do nothing, and the run batched anyway.
func TestBatchSizeOverrideActsOnBeingGiven(t *testing.T) {
	t.Run("zero turns batching off", func(t *testing.T) {
		cfg := batchedConfig()
		require.NoError(t, applyBatchSize(cfg, 0, true))
		assert.Zero(t, cfg.BatchSize, "the flag was given as 0, so the config's 10 must not survive it")
	})

	t.Run("a value replaces the config's", func(t *testing.T) {
		cfg := batchedConfig()
		require.NoError(t, applyBatchSize(cfg, 4, true))
		assert.Equal(t, 4, cfg.BatchSize)
	})

	t.Run("not given leaves the config alone", func(t *testing.T) {
		cfg := batchedConfig()
		require.NoError(t, applyBatchSize(cfg, 0, false))
		assert.Equal(t, 10, cfg.BatchSize, "the flag's default must not override the file")
	})

	// A negative value has to reach validation rather than be dropped on the
	// way, or it is accepted and then ignored.
	t.Run("a negative value is rejected", func(t *testing.T) {
		cfg := batchedConfig()
		err := applyBatchSize(cfg, -1, true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "batch_size cannot be negative")
	})

	// An override still has to fit the rest of the config.
	t.Run("an override that contradicts the config is rejected", func(t *testing.T) {
		cfg := batchedConfig()
		cfg.RPS = 0
		cfg.Iterations = 500
		err := applyBatchSize(cfg, 5, true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be combined with iterations")
	})
}

// The flag the override reads has to exist, and has to arrive unset.
func TestBatchSizeFlagIsRegistered(t *testing.T) {
	flag := benchmarkCmd.Flags().Lookup("batch-size")
	require.NotNil(t, flag, "--batch-size is what the override is gated on")
	assert.Equal(t, "0", flag.DefValue)
	assert.False(t, benchmarkCmd.Flags().Changed("batch-size"), "an unused flag must not count as given")
}
