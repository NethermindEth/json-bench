package cmd

import (
	"bytes"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/types"
)

// The summary line is what an operator actually reads, and it used to derive its
// ok count as requests minus errors. Since a null result is not an error, every
// rpc_null was absorbed into ok and the class was never printed — hiding the one
// failure mode the separate tally exists to expose.
func TestLogOutcomesNamesNullResults(t *testing.T) {
	var buf bytes.Buffer
	previous := logger
	logger = logrus.New()
	logger.SetOutput(&buf)
	logger.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
	t.Cleanup(func() { logger = previous })

	logOutcomes(&types.BenchmarkResult{
		ClientMetrics: map[string]*types.ClientMetrics{
			"stub": {
				Name:          "stub",
				TotalRequests: 4000,
				TotalErrors:   313,
				ErrorRate:     7.825,
				Outcomes: map[string]int64{
					"ok": 3437, "rpc_null": 250, "rpc_error": 232, "http_error": 81,
				},
				Latency: types.MetricSummary{P99: 22.7},
			},
		},
	})

	out := buf.String()
	require.Contains(t, out, "4000 requests")
	assert.Contains(t, out, "3437 ok")
	assert.Contains(t, out, "250 rpc_null")
	assert.Contains(t, out, "232 rpc_error")
	assert.Contains(t, out, "81 http_error")
	assert.NotContains(t, out, "3687 ok", "nulls must not be folded into the ok count")
}
