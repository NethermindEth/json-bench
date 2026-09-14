package engine

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Exact percentiles are computed from every observation, so the accumulator's
// memory is linear in the request count. That is a deliberate trade, but it has
// to stay bounded: the engine used to keep a second copy of every value in a
// client-wide group, which doubled the cost for a distribution that can be
// concatenated back from the keys. On a run that shares a host with the node it
// measures, the generator's footprint is inside the measurement.
func TestAccumulatorRetentionStaysBounded(t *testing.T) {
	const samples = 200_000

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	acc := NewAccumulator()
	for i := 0; i < samples; i++ {
		acc.Add(Sample{
			Client: "c", Name: "call", Method: "eth_call", Status: 200, Outcome: OutcomeOK,
			Phases: Phases{Waiting: time.Duration(i%1000) * time.Microsecond},
		})
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(acc)

	perSample := float64(after.HeapAlloc-before.HeapAlloc) / samples
	t.Log(fmt.Sprintf("retained %.0f bytes per sample (budget %d)", perSample, retainedBytesPerSample))

	assert.Less(t, perSample, float64(retainedBytesPerSample)*1.35,
		"retention grew past its budget; check for a second copy of the observations")
}

// The client-wide distribution is concatenated from the keyed groups, so it has
// to still be the same set of observations.
func TestClientWideDistributionMatchesTheKeyedGroups(t *testing.T) {
	acc := NewAccumulator()
	for i := 0; i < 300; i++ {
		name := "fast"
		waiting := time.Millisecond
		if i%3 == 0 {
			name, waiting = "slow", 50*time.Millisecond
		}
		acc.Add(Sample{
			Client: "c", Name: name, Method: "eth_call", Status: 200, Outcome: OutcomeOK,
			Phases: Phases{Waiting: waiting},
		})
	}

	cm := acc.ClientMetrics("c", Delivery{})
	assert.EqualValues(t, 300, cm.Latency.Count, "every observation reaches the client summary")

	var perCall int64
	for _, call := range cm.Calls {
		perCall += call.Count
	}
	assert.EqualValues(t, 300, perCall)
	assert.Greater(t, cm.Latency.Max, 40.0, "the slow call's observations are in the client distribution")
	assert.Less(t, cm.Latency.Min, 2.0, "so are the fast call's")
}

// ClientMetrics is asked for a client that recorded nothing, which is what a
// target that never answered looks like.
func TestClientMetricsForATargetWithNoSamples(t *testing.T) {
	acc := NewAccumulator()
	cm := acc.ClientMetrics("never-ran", Delivery{Scheduled: 10})
	assert.NotNil(t, cm)
	assert.Zero(t, cm.TotalRequests)
	assert.Empty(t, cm.Calls)
	assert.Zero(t, cm.Latency.Count)
}

// The concatenated client distribution must survive a client whose keyed
// groups exist but hold no values for a phase.
func TestPhaseValuesWithNoObservations(t *testing.T) {
	acc := NewAccumulator()
	acc.Add(Sample{Client: "c", Name: "n", Method: "m", Status: 200, Outcome: OutcomeOK})
	cm := acc.ClientMetrics("c", Delivery{})
	assert.EqualValues(t, 1, cm.Latency.Count)
	assert.Zero(t, cm.ConnectionMetrics.TLSHandshakeTime, "no TLS observed means zero, not a panic")
}
