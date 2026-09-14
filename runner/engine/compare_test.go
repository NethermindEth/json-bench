package engine

import (
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lognormalSample draws latencies with the shape real RPC latency has: skewed,
// heavy-tailed, never negative. A test assuming normality would be answering a
// question about a distribution the data does not have.
func lognormalSample(rng *rand.Rand, n int, medianMs, sigma float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = medianMs * math.Exp(sigma*rng.NormFloat64())
	}
	return out
}

func TestMannWhitneyFindsNoDifferenceWhenThereIsNone(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	a := lognormalSample(rng, 800, 20, 0.4)
	b := lognormalSample(rng, 800, 20, 0.4)

	_, _, p := mannWhitney(a, b)
	assert.Greater(t, p, 0.05, "two draws from one distribution must not look different")
}

func TestMannWhitneyFindsAShiftThatIsThere(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	a := lognormalSample(rng, 800, 20, 0.4)
	b := lognormalSample(rng, 800, 26, 0.4)

	_, z, p := mannWhitney(a, b)
	assert.Less(t, p, 0.001, "a 30%% shift over 800 samples each must be detected")
	assert.Less(t, z, 0.0, "the first sample ranks lower, so z is negative")
}

// Latency values tie constantly once they are recorded in milliseconds.
// Ignoring ties inflates the variance and understates significance.
func TestMannWhitneyCorrectsForTies(t *testing.T) {
	a := make([]float64, 200)
	b := make([]float64, 200)
	for i := range a {
		a[i] = 10
		b[i] = 10
	}

	u, z, p := mannWhitney(a, b)
	assert.Equal(t, 1.0, p, "two identical constant samples carry no information")
	assert.Zero(t, z)
	assert.Equal(t, float64(len(a)*len(b))/2, u)
}

func TestMannWhitneyHandlesDegenerateInput(t *testing.T) {
	_, _, p := mannWhitney(nil, []float64{1, 2, 3})
	assert.Equal(t, 1.0, p)

	_, _, p = mannWhitney([]float64{1}, []float64{2})
	assert.LessOrEqual(t, p, 1.0)
	assert.GreaterOrEqual(t, p, 0.0)
}

func TestTwoSidedPIsBounded(t *testing.T) {
	for _, z := range []float64{-40, -3, -1, 0, 1, 3, 40, math.Inf(1), math.Inf(-1)} {
		p := twoSidedP(z)
		assert.GreaterOrEqual(t, p, 0.0, "z=%v", z)
		assert.LessOrEqual(t, p, 1.0, "z=%v", z)
	}
	assert.InDelta(t, 1.0, twoSidedP(0), 1e-9)
	assert.InDelta(t, 0.0455, twoSidedP(2), 0.001)
}

func TestCompareSamplesReportsEffectSizeAndDirection(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	fast := lognormalSample(rng, 600, 20, 0.3)
	slow := lognormalSample(rng, 600, 30, 0.3)

	c := compareSamples("eth_call", "fast", "slow", fast, slow)

	assert.Equal(t, "eth_call", c.Method)
	assert.Equal(t, 600, c.CountA)
	assert.InDelta(t, 20, c.MedianAMs, 2)
	assert.InDelta(t, 30, c.MedianBMs, 3)
	assert.Greater(t, c.MedianShiftMs, 0.0)
	assert.InDelta(t, 50, c.MedianShiftPercent, 15, "a 20ms to 30ms shift is about +50%%")
	assert.Equal(t, "fast", c.Faster)
	assert.Less(t, c.PValue, 0.001)
	assert.Greater(t, c.P99BMs, c.P99AMs)
}

// Naming a winner on a difference that could be noise is the failure mode the
// old p-value matrix had: it produced a number for any input.
func TestCompareSamplesNamesNoWinnerWithoutEvidence(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	a := lognormalSample(rng, 400, 20, 0.4)
	b := lognormalSample(rng, 400, 20, 0.4)

	c := compareSamples("eth_call", "a", "b", a, b)
	assert.Empty(t, c.Faster)
	assert.Greater(t, c.PValue, 0.05)
}

func TestCompareClientsNeedsTwoClientsAndEnoughSamples(t *testing.T) {
	t.Run("one client", func(t *testing.T) {
		accum := NewAccumulator()
		for i := 0; i < 100; i++ {
			accum.Add(sampleWith("only", "eth_call", 10))
		}
		assert.Empty(t, accum.CompareClients())
	})

	t.Run("too few samples", func(t *testing.T) {
		accum := NewAccumulator()
		for i := 0; i < minComparisonSamples-1; i++ {
			accum.Add(sampleWith("a", "eth_call", 10))
			accum.Add(sampleWith("b", "eth_call", 20))
		}
		assert.Empty(t, accum.CompareClients(),
			"below the sample floor the normal approximation does not hold, so no p-value is reported")
	})

	t.Run("enough of both", func(t *testing.T) {
		accum := NewAccumulator()
		rng := rand.New(rand.NewSource(5))
		for _, v := range lognormalSample(rng, 200, 10, 0.3) {
			accum.Add(sampleWith("a", "eth_call", v))
		}
		for _, v := range lognormalSample(rng, 200, 40, 0.3) {
			accum.Add(sampleWith("b", "eth_call", v))
		}

		comparisons := accum.CompareClients()
		require.Len(t, comparisons, 1)
		assert.Equal(t, "a", comparisons[0].ClientA, "clients are ordered so the report is stable")
		assert.Equal(t, "b", comparisons[0].ClientB)
		assert.Equal(t, "a", comparisons[0].Faster)
	})
}

// Comparisons must be grouped by method: a client can be faster on one method
// and slower on another, and an aggregate would hide it.
func TestCompareClientsIsPerMethod(t *testing.T) {
	accum := NewAccumulator()
	rng := rand.New(rand.NewSource(6))

	for _, v := range lognormalSample(rng, 150, 10, 0.2) {
		accum.Add(sampleWith("a", "eth_call", v))
	}
	for _, v := range lognormalSample(rng, 150, 30, 0.2) {
		accum.Add(sampleWith("b", "eth_call", v))
	}
	for _, v := range lognormalSample(rng, 150, 50, 0.2) {
		accum.Add(sampleWith("a", "eth_getLogs", v))
	}
	for _, v := range lognormalSample(rng, 150, 20, 0.2) {
		accum.Add(sampleWith("b", "eth_getLogs", v))
	}

	comparisons := accum.CompareClients()
	require.Len(t, comparisons, 2)

	byMethod := map[string]string{}
	for _, c := range comparisons {
		byMethod[c.Method] = c.Faster
	}
	assert.Equal(t, "a", byMethod["eth_call"])
	assert.Equal(t, "b", byMethod["eth_getLogs"])
}

// sampleWith builds a minimal successful sample with a given service time.
func sampleWith(client, method string, ms float64) Sample {
	return Sample{
		Client:  client,
		Method:  method,
		Name:    method,
		Status:  200,
		Outcome: OutcomeOK,
		Phases:  Phases{Waiting: time.Duration(ms * float64(time.Millisecond))},
	}
}
