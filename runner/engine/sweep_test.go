package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/internal/stubnode"
	"github.com/jsonrpc-bench/runner/types"
)

func sweepConfig(url string) *config.Config {
	return runConfig(url, []*config.Call{
		{Name: "eth_call", Method: "eth_call", Params: []any{}, Weight: 1},
	}, func(c *config.Config) {
		c.Duration = "1s"
		c.RPS = 200
		c.VUs = 40
	})
}

func TestParseSweepDimensionRejectsUnsweepableSettings(t *testing.T) {
	_, err := ParseSweepDimension("seed")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rps")

	d, err := ParseSweepDimension("batch_size")
	require.NoError(t, err)
	assert.Equal(t, SweepBatchSize, d)
}

// Drawing a fresh sequence per setting would compare workloads rather than
// settings, so every run in a sweep replays the same requests.
func TestSweepPinsOneRequestSequenceForEveryRun(t *testing.T) {
	stubCfg := stubnode.DefaultConfig()
	stubCfg.Default.Latency = &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 1}
	srv := stubServer(t, stubCfg)

	dir := t.TempDir()
	opts := testOptions(t)
	opts.OutputDir = dir

	result, err := Sweep(context.Background(), sweepConfig(srv.URL), opts, SweepOptions{
		Dimension: SweepBatchSize,
		Values:    []string{"1", "5"},
		OutputDir: dir,
	})
	require.NoError(t, err)
	require.Len(t, result.Points, 2)

	shared := filepath.Join(dir, "requests.csv")
	_, err = os.Stat(shared)
	require.NoError(t, err, "the sweep pins one sequence on disk")

	for _, point := range result.Points {
		assert.EqualValues(t, result.Points[0].Requests, point.Requests,
			"every setting drove the same number of requests")
	}
}

// rps stays a rate of requests, so batching changes how many round trips carry
// the load, not how much load there is.
func TestSweepReportsRequestRateNotDispatchRate(t *testing.T) {
	stubCfg := stubnode.DefaultConfig()
	stubCfg.Default.Latency = &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 1}
	srv := stubServer(t, stubCfg)

	dir := t.TempDir()
	opts := testOptions(t)
	opts.OutputDir = dir

	result, err := Sweep(context.Background(), sweepConfig(srv.URL), opts, SweepOptions{
		Dimension: SweepBatchSize,
		Values:    []string{"1", "10"},
		OutputDir: dir,
	})
	require.NoError(t, err)
	require.Len(t, result.Points, 2)

	for _, point := range result.Points {
		assert.InDelta(t, 200, point.AchievedRPS, 60,
			"%s should report ~200 requests per second, not the dispatch rate", point.Label)
		assert.InDelta(t, 100, point.DeliveredPct, 1, "%s", point.Label)
	}
}

// A sweep whose sequence was sized for the smallest setting would leave the
// larger ones short, and a run that ran out of requests looks exactly like one
// that was simply faster.
func TestSweepSizesTheSequenceForTheHungriestSetting(t *testing.T) {
	stubCfg := stubnode.DefaultConfig()
	stubCfg.Default.Latency = &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 1}
	srv := stubServer(t, stubCfg)

	dir := t.TempDir()
	opts := testOptions(t)
	opts.OutputDir = dir

	result, err := Sweep(context.Background(), sweepConfig(srv.URL), opts, SweepOptions{
		Dimension: SweepRPS,
		Values:    []string{"50", "400"},
		OutputDir: dir,
	})
	require.NoError(t, err)

	byLabel := map[string]SweepPoint{}
	for _, p := range result.Points {
		byLabel[p.Label] = p
	}
	assert.InDelta(t, 50, byLabel["rps=50"].AchievedRPS, 15)
	assert.InDelta(t, 400, byLabel["rps=400"].AchievedRPS, 80,
		"the high-rate point must not be cut short by a sequence drawn for the low one")
}

func TestSweepNeedsSomethingToCompare(t *testing.T) {
	_, err := Sweep(context.Background(), sweepConfig("http://127.0.0.1:1"), testOptions(t), SweepOptions{
		Dimension: SweepRPS,
		Values:    []string{"100"},
		OutputDir: t.TempDir(),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least two values")
}

// A single pass cannot tell a difference between settings from the target
// drifting underneath them.
func TestSweepWarnsThatOnePassCannotSeparateDrift(t *testing.T) {
	stubCfg := stubnode.DefaultConfig()
	stubCfg.Default.Latency = &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 1}
	srv := stubServer(t, stubCfg)

	dir := t.TempDir()
	opts := testOptions(t)
	opts.OutputDir = dir

	result, err := Sweep(context.Background(), sweepConfig(srv.URL), opts, SweepOptions{
		Dimension: SweepBatchSize,
		Values:    []string{"1", "2"},
		OutputDir: dir,
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.Warnings)
	assert.Contains(t, result.Warnings[0], "--repeat")
}

// A point the generator could not offer measures the generator, which is the
// same trap the rate search guards against.
func TestSweepFlagsUndeliveredPointsAsInconclusive(t *testing.T) {
	point := &SweepPoint{Passes: []SweepPass{{Delivered: 62.5, P99Ms: 100}}}
	finalisePoint(point)
	assert.True(t, point.Inconclusive)
	assert.Contains(t, point.InconclusiveB, "measures the generator")

	fine := &SweepPoint{Passes: []SweepPass{{Delivered: 100, P99Ms: 10}}}
	finalisePoint(fine)
	assert.False(t, fine.Inconclusive)
}

func TestSweepReportsTheSpreadBetweenPasses(t *testing.T) {
	point := &SweepPoint{Passes: []SweepPass{
		{P99Ms: 100}, {P99Ms: 150}, {P99Ms: 120},
	}}
	finalisePoint(point)
	assert.Equal(t, 120.0, point.P99Ms, "the median of the passes")
	assert.InDelta(t, 41.7, point.SpreadP99Pct, 0.5, "(150-100)/120")
}

// A target that changed identity mid-sweep means the points are not comparing
// settings any more.
func TestSweepDetectsTheTargetChangingUnderneathIt(t *testing.T) {
	first := types.RunManifest{Clients: []types.ClientProvenance{
		{Name: "n", ClientVersion: "Nethermind/1.0", ChainID: "1"},
	}}
	same := types.RunManifest{Clients: []types.ClientProvenance{
		{Name: "n", ClientVersion: "Nethermind/1.0", ChainID: "1"},
	}}
	upgraded := types.RunManifest{Clients: []types.ClientProvenance{
		{Name: "n", ClientVersion: "Nethermind/1.1", ChainID: "1"},
	}}

	assert.Empty(t, targetChanged(first, same))
	assert.Contains(t, targetChanged(first, upgraded), "not all measuring the same target")
}

func TestSweepDimensionApplyClearsConflictingLoadShape(t *testing.T) {
	cfg := &config.Config{Iterations: 500, Stages: []config.Stage{{Duration: "1m", Target: 10}}}
	label, err := SweepRPS.apply(cfg, "250")
	require.NoError(t, err)
	assert.Equal(t, "rps=250", label)
	assert.Equal(t, 250, cfg.RPS)
	assert.Zero(t, cfg.Iterations, "a rate sweep must not leave the iteration mode set")
	assert.Empty(t, cfg.Stages, "nor a ramp that would ignore the rate")

	_, err = SweepDuration.apply(&config.Config{}, "soon")
	assert.Error(t, err)

	_, err = SweepBatchSize.apply(&config.Config{}, "-1")
	assert.Error(t, err)
}

// The result table carries one figure per setting, so several targets would be
// reduced to whichever the summary happened to read.
func TestSweepRefusesSeveralTargets(t *testing.T) {
	cfg := sweepConfig("http://127.0.0.1:1")
	cfg.ResolvedClients = append(cfg.ResolvedClients, &types.ClientConfig{
		Name: "second", URL: "http://127.0.0.1:2",
	})

	_, err := Sweep(context.Background(), cfg, testOptions(t), SweepOptions{
		Dimension: SweepRPS,
		Values:    []string{"100", "200"},
		OutputDir: t.TempDir(),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "one target at a time")
}
