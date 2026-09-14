package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/config"
)

// Batching divides the rate, so a short run at a low rate can round down to no
// arrivals at all. A run that completes instantly having contacted nothing is
// worse than one that refuses to start.
func TestScheduleRefusesARateThatRoundsToNothing(t *testing.T) {
	_, err := newSchedule(&config.Config{RPS: 1, Duration: "1s", VUs: 4}, 1000, 2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schedules no requests")

	// The same rate over a long enough run is fine.
	s, err := newSchedule(&config.Config{RPS: 1, Duration: "10s", VUs: 4}, 1000, 2)
	require.NoError(t, err)
	assert.Equal(t, 5, s.total)
}
