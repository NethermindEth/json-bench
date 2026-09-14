package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/engine"
)

func TestParseVary(t *testing.T) {
	dimension, values, err := parseVary("batch_size=1, 5 ,20")
	require.NoError(t, err)
	assert.Equal(t, engine.SweepBatchSize, dimension)
	assert.Equal(t, []string{"1", "5", "20"}, values, "surrounding space is not part of a value")

	for _, spec := range []string{
		"batch_size",  // no values
		"rps=100",     // nothing to compare against
		"seed=1,2",    // not sweepable
		"rps=100,100", // the same point twice
		"",            // empty
	} {
		_, _, err := parseVary(spec)
		assert.Error(t, err, "spec %q should be rejected", spec)
	}
}
