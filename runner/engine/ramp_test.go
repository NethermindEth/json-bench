package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/internal/stubnode"
)

// countIn reports how many offsets fall inside a window, which is how the
// realised rate over that window is checked against the configured curve.
func countIn(offsets []time.Duration, from, to time.Duration) int {
	var n int
	for _, offset := range offsets {
		if offset >= from && offset < to {
			n++
		}
	}
	return n
}

func TestRampOffsetsHoldAConstantRate(t *testing.T) {
	offsets, total, err := RampOffsets(100, []config.Stage{{Duration: "2s", Target: 100}})
	require.NoError(t, err)

	assert.Equal(t, 2*time.Second, total)
	assert.InDelta(t, 200, len(offsets), 2, "100 rps for 2s")
	assert.InDelta(t, 100, countIn(offsets, 0, time.Second), 3, "the first second")
	assert.InDelta(t, 100, countIn(offsets, time.Second, 2*time.Second), 3, "the second second")
}

// The area under a line from 0 to R over D is R*D/2, so a ramp issues half what
// holding the target would.
func TestRampOffsetsFollowALinearRamp(t *testing.T) {
	offsets, total, err := RampOffsets(0, []config.Stage{{Duration: "4s", Target: 100}})
	require.NoError(t, err)

	assert.Equal(t, 4*time.Second, total)
	assert.InDelta(t, 200, len(offsets), 4, "the area under 0->100 over 4s")

	// Each successive second carries more of the load than the one before.
	first := countIn(offsets, 0, time.Second)
	second := countIn(offsets, time.Second, 2*time.Second)
	third := countIn(offsets, 2*time.Second, 3*time.Second)
	fourth := countIn(offsets, 3*time.Second, 4*time.Second)

	assert.Less(t, first, second)
	assert.Less(t, second, third)
	assert.Less(t, third, fourth)

	// The rate at the midpoint of each second is 12.5, 37.5, 62.5, 87.5.
	assert.InDelta(t, 12.5, first, 3)
	assert.InDelta(t, 87.5, fourth, 3)
}

func TestRampOffsetsClimbAStaircase(t *testing.T) {
	offsets, total, err := RampOffsets(50, []config.Stage{
		{Duration: "1s", Target: 50},
		{Duration: "1s", Target: 100},
		{Duration: "1s", Target: 100},
	})
	require.NoError(t, err)

	assert.Equal(t, 3*time.Second, total)
	assert.InDelta(t, 50, countIn(offsets, 0, time.Second), 3, "held at 50")
	assert.InDelta(t, 75, countIn(offsets, time.Second, 2*time.Second), 4, "ramping 50 to 100 averages 75")
	assert.InDelta(t, 100, countIn(offsets, 2*time.Second, 3*time.Second), 4, "held at 100")
}

func TestRampOffsetsRampDownToZero(t *testing.T) {
	offsets, _, err := RampOffsets(100, []config.Stage{
		{Duration: "1s", Target: 100},
		{Duration: "1s", Target: 0},
	})
	require.NoError(t, err)

	assert.Greater(t, countIn(offsets, 0, time.Second), countIn(offsets, time.Second, 2*time.Second))
	assert.InDelta(t, 50, countIn(offsets, time.Second, 2*time.Second), 4, "ramping 100 to 0 averages 50")
}

func TestRampOffsetsAreOrdered(t *testing.T) {
	offsets, _, err := RampOffsets(10, []config.Stage{
		{Duration: "500ms", Target: 200},
		{Duration: "500ms", Target: 10},
	})
	require.NoError(t, err)
	require.NotEmpty(t, offsets)

	for i := 1; i < len(offsets); i++ {
		assert.GreaterOrEqual(t, offsets[i], offsets[i-1], "arrivals must be monotonic")
	}
}

func TestScheduleRejectsAWarmupThatSwallowsTheRun(t *testing.T) {
	_, err := newSchedule(&config.Config{Duration: "10s", RPS: 10, Warmup: "10s"}, 1000)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no measured window")

	_, err = newSchedule(&config.Config{
		RPS:    10,
		Warmup: "5s",
		Stages: []config.Stage{{Duration: "2s", Target: 10}},
	}, 1000)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no measured window")
}

func TestScheduleMarksWarmupRequests(t *testing.T) {
	sched, err := newSchedule(&config.Config{Duration: "10s", RPS: 100, Warmup: "2s"}, 10_000)
	require.NoError(t, err)

	start := time.Now()
	assert.True(t, sched.isWarmup(start, sched.due(start, 0)))
	assert.True(t, sched.isWarmup(start, sched.due(start, 150)), "1.5s in")
	assert.False(t, sched.isWarmup(start, sched.due(start, 200)), "exactly at the boundary is measured")
	assert.False(t, sched.isWarmup(start, sched.due(start, 500)))
}

func TestScheduleWithoutWarmupMarksNothing(t *testing.T) {
	sched, err := newSchedule(&config.Config{Duration: "5s", RPS: 10}, 1000)
	require.NoError(t, err)

	start := time.Now()
	assert.False(t, sched.isWarmup(start, sched.due(start, 0)))
	assert.Equal(t, start, sched.measureFrom(start))
}

// The reason warmup exists: a slow first period must not be averaged into the
// steady-state percentiles. The stub is configured so the two periods are
// unmistakably different, and the reported p99 has to reflect only the second.
func TestWarmupIsExcludedFromTheReportedLatency(t *testing.T) {
	stubCfg := stubnode.DefaultConfig()
	stubCfg.Default.Latency = &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 4}
	srv := stubServer(t, stubCfg)

	calls := []*config.Call{{Name: "eth_call", Method: "eth_call", Params: []any{}, Weight: 1}}

	withWarmup := runConfig(srv.URL, calls, func(c *config.Config) {
		c.Duration = "2s"
		c.RPS = 100
		c.VUs = 20
		c.Warmup = "1s"
	})

	result, _, err := Run(context.Background(), withWarmup, testOptions(t))
	require.NoError(t, err)

	client := result.ClientMetrics["stub"]
	d := client.Delivery

	assert.NotZero(t, d.WarmupSent, "the warmup second must have issued requests")
	assert.InDelta(t, 100, d.WarmupSent, 15, "100 rps for the warmup second")
	assert.InDelta(t, 100, d.Scheduled, 15, "only the measured second is scheduled work")
	assert.EqualValues(t, d.Sent, client.TotalRequests,
		"the reported request count covers the measured window only")

	// Throughput divides by the measured window, not the whole run.
	assert.InDelta(t, 1.0, d.ElapsedSeconds, 0.4)
}

func TestWarmupSamplesAreKeptInTheSampleFile(t *testing.T) {
	stubCfg := stubnode.DefaultConfig()
	stubCfg.Default.Latency = &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 2}
	srv := stubServer(t, stubCfg)

	cfg := runConfig(srv.URL, []*config.Call{
		{Name: "eth_call", Method: "eth_call", Params: []any{}, Weight: 1},
	}, func(c *config.Config) {
		c.Duration = "2s"
		c.RPS = 50
		c.VUs = 10
		c.Warmup = "1s"
	})

	opts := testOptions(t)
	result, _, err := Run(context.Background(), cfg, opts)
	require.NoError(t, err)

	samples := readSampleFile(t, opts.OutputDir)

	var warmup, measured int
	for _, s := range samples {
		if s.Warmup {
			warmup++
			continue
		}
		measured++
	}

	assert.NotZero(t, warmup, "warmup requests are discarded from the statistics but not thrown away")
	assert.EqualValues(t, result.ClientMetrics["stub"].TotalRequests, measured,
		"the measured samples are exactly what the report counted")
}

func TestStagedRunReportsItsShape(t *testing.T) {
	stubCfg := stubnode.DefaultConfig()
	stubCfg.Default.Latency = &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 2}
	srv := stubServer(t, stubCfg)

	cfg := runConfig(srv.URL, []*config.Call{
		{Name: "eth_call", Method: "eth_call", Params: []any{}, Weight: 1},
	}, func(c *config.Config) {
		c.Duration = ""
		c.RPS = 20
		c.VUs = 30
		c.Stages = []config.Stage{
			{Duration: "1s", Target: 20},
			{Duration: "1s", Target: 60},
		}
	})

	result, _, err := Run(context.Background(), cfg, testOptions(t))
	require.NoError(t, err)

	client := result.ClientMetrics["stub"]
	// 20 rps for a second, then averaging 40 over the ramp.
	assert.InDelta(t, 60, client.TotalRequests, 12)
	assert.Zero(t, client.Delivery.Dropped)

	load, ok := result.Summary["load"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 2, load["stages"])
	assert.Equal(t, "2s", load["duration"], "the run's length is the sum of its stages")
}

// readSampleFile decodes the samples a run wrote.
func readSampleFile(t *testing.T, dir string) []Sample {
	t.Helper()
	file, err := os.Open(filepath.Join(dir, SampleFilename))
	require.NoError(t, err)
	defer file.Close()

	samples, err := ReadSamples(file)
	require.NoError(t, err)
	return samples
}

// A request placed at the very end of the window must still be sent. Dropping
// it would report a shortfall the generator did not have, and drops are what
// the rate search reads to decide whether a probe means anything.
func TestTheLastArrivalOfTheWindowIsNotDropped(t *testing.T) {
	stubCfg := stubnode.DefaultConfig()
	stubCfg.Default.Latency = &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 1}
	srv := stubServer(t, stubCfg)

	for attempt := 0; attempt < 6; attempt++ {
		cfg := runConfig(srv.URL, []*config.Call{
			{Name: "eth_call", Method: "eth_call", Params: []any{}, Weight: 1},
		}, func(c *config.Config) {
			c.Duration = ""
			c.RPS = 40
			c.VUs = 40
			c.Stages = []config.Stage{{Duration: "500ms", Target: 40}}
		})

		result, _, err := Run(context.Background(), cfg, testOptions(t))
		require.NoError(t, err)

		d := result.ClientMetrics["stub"].Delivery
		require.Zero(t, d.Dropped, "attempt %d dropped %d of %d scheduled", attempt, d.Dropped, d.Scheduled)
	}
}

// The offered rate is a rate over the dispatch window. Dividing by an elapsed
// time that also covers draining in-flight work understates the rate the
// generator managed to offer, by seconds when the endpoint is queueing.
func TestAchievedRateMeasuresTheDispatchWindow(t *testing.T) {
	start := time.Now()
	d := Delivery{
		Sent:         100,
		Started:      start,
		LastDispatch: start.Add(time.Second),
		Finished:     start.Add(5 * time.Second),
	}

	assert.Equal(t, time.Second, d.OfferedWindow())
	assert.Equal(t, 5*time.Second, d.Elapsed())
	assert.InDelta(t, 100, d.AchievedRate(), 0.001, "100 requests offered over one second")

	// Without a recorded dispatch it falls back to the elapsed time rather
	// than reporting nothing.
	noDispatch := Delivery{Sent: 100, Started: start, Finished: start.Add(2 * time.Second)}
	assert.Equal(t, 2*time.Second, noDispatch.OfferedWindow())
	assert.InDelta(t, 50, noDispatch.AchievedRate(), 0.001)
}

func TestAchievedRateIgnoresWarmupDispatches(t *testing.T) {
	stubCfg := stubnode.DefaultConfig()
	stubCfg.Default.Latency = &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 2}
	srv := stubServer(t, stubCfg)

	cfg := runConfig(srv.URL, []*config.Call{
		{Name: "eth_call", Method: "eth_call", Params: []any{}, Weight: 1},
	}, func(c *config.Config) {
		c.Duration = "3s"
		c.RPS = 50
		c.VUs = 20
		c.Warmup = "1s"
	})

	result, _, err := Run(context.Background(), cfg, testOptions(t))
	require.NoError(t, err)

	d := result.ClientMetrics["stub"].Delivery
	assert.InDelta(t, 50, d.AchievedRPS, 12, "the rate over the measured window, not over the whole run")
	assert.Greater(t, d.OfferedSeconds, 0.0)
	assert.LessOrEqual(t, d.OfferedSeconds, d.ElapsedSeconds+0.001)
}
