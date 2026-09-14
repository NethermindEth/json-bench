package engine

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/types"
)

// writeRun lays down the two artifacts a report is rebuilt from.
func writeRun(t *testing.T, manifest types.RunManifest, samples []Sample) string {
	t.Helper()
	dir := t.TempDir()

	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ManifestFilename), raw, 0o600))

	writer, err := NewSampleWriter(filepath.Join(dir, SampleFilename))
	require.NoError(t, err)
	for _, s := range samples {
		require.NoError(t, writer.Write(s))
	}
	require.NoError(t, writer.Close())
	return dir
}

func sampleAt(name, method string, waiting time.Duration, outcome Outcome) Sample {
	return Sample{
		Client: "c", Name: name, Method: method, Status: 200, Outcome: outcome,
		Start:  time.Now(),
		Phases: Phases{Sending: 30 * time.Microsecond, Waiting: waiting, Receiving: 80 * time.Microsecond},
	}
}

func manifest() types.RunManifest {
	return types.RunManifest{
		Engine: "native", EngineVersion: Version, TestName: "t", Seed: 1,
		Saturation: "queue", TargetRPS: 100, Concurrency: 10, Duration: "1m",
		ErrorRateSemantics: ErrorRateSemanticsRPCAware,
	}
}

func TestCallReportRebuildsFromSamples(t *testing.T) {
	var samples []Sample
	for i := 0; i < 100; i++ {
		samples = append(samples, sampleAt("fast", "eth_getProof", time.Millisecond, OutcomeOK))
	}
	for i := 0; i < 50; i++ {
		outcome := OutcomeOK
		if i < 5 {
			outcome = OutcomeRPCError
		}
		samples = append(samples, sampleAt("slow", "eth_getProof", 100*time.Millisecond, outcome))
	}

	run, err := LoadRun(writeRun(t, manifest(), samples))
	require.NoError(t, err)

	calls := run.CallReport(ReportOptions{})
	require.Len(t, calls, 2, "two calls sharing one method stay apart")

	byName := map[string]*CallStats{}
	for _, c := range calls {
		byName[c.Name] = c
	}
	assert.EqualValues(t, 100, byName["fast"].Count)
	assert.EqualValues(t, 50, byName["slow"].Count)
	assert.EqualValues(t, 5, byName["slow"].Errors)
	assert.Equal(t, "eth_getProof", byName["slow"].Method)

	// The phase split is the point: a difference confined to waiting is the node.
	assert.InDelta(t, 1.0, byName["fast"].Waiting.Avg, 0.2)
	assert.InDelta(t, 100.0, byName["slow"].Waiting.Avg, 1.0)
	assert.Less(t, byName["slow"].Sending.Avg, 1.0)

	totals := run.Totals(ReportOptions{})
	assert.EqualValues(t, 150, totals.Count)
	assert.EqualValues(t, 5, totals.Errors)
	assert.EqualValues(t, 5, totals.Outcomes[string(OutcomeRPCError)])
}

func TestReportExcludesWarmupUnlessAsked(t *testing.T) {
	samples := []Sample{
		sampleAt("call", "eth_call", time.Millisecond, OutcomeOK),
		sampleAt("call", "eth_call", time.Millisecond, OutcomeOK),
	}
	samples[0].Warmup = true

	run, err := LoadRun(writeRun(t, manifest(), samples))
	require.NoError(t, err)

	assert.EqualValues(t, 1, run.Totals(ReportOptions{}).Count, "warmup is excluded by default")
	assert.EqualValues(t, 2, run.Totals(ReportOptions{Warmup: true}).Count)
}

// Latency mixed with fast failures describes neither, so successes can be
// isolated without re-running.
func TestReportCanRestrictToSuccesses(t *testing.T) {
	samples := []Sample{
		sampleAt("call", "eth_call", 100*time.Millisecond, OutcomeOK),
		sampleAt("call", "eth_call", time.Microsecond, OutcomeRPCError),
	}
	run, err := LoadRun(writeRun(t, manifest(), samples))
	require.NoError(t, err)

	all := run.Totals(ReportOptions{})
	ok := run.Totals(ReportOptions{Outcome: string(OutcomeOK)})
	assert.EqualValues(t, 2, all.Count)
	assert.EqualValues(t, 1, ok.Count)
	assert.Greater(t, ok.Duration.Avg, all.Duration.Avg,
		"dropping the fast failure raises the average of what actually worked")
}

func TestLoadRunRejectsARunWithoutSamples(t *testing.T) {
	dir := t.TempDir()
	raw, err := json.Marshal(manifest())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ManifestFilename), raw, 0o600))

	_, err = LoadRun(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--no-samples")
}

func drawSamples(t *testing.T, name string, n int, centre time.Duration, seed int64) []Sample {
	t.Helper()
	rng := rand.New(rand.NewSource(seed))
	out := make([]Sample, 0, n)
	for i := 0; i < n; i++ {
		jitter := time.Duration(rng.NormFloat64() * float64(centre) * 0.1)
		out = append(out, sampleAt(name, "eth_call", centre+jitter, OutcomeOK))
	}
	return out
}

func TestCompareRunsFindsAShiftAndItsDirection(t *testing.T) {
	baseDir := writeRun(t, manifest(), drawSamples(t, "call", 400, 10*time.Millisecond, 1))
	currDir := writeRun(t, manifest(), drawSamples(t, "call", 400, 20*time.Millisecond, 2))

	baseline, err := LoadRun(baseDir)
	require.NoError(t, err)
	current, err := LoadRun(currDir)
	require.NoError(t, err)

	diff := CompareRuns(baseline, current, ReportOptions{})
	require.True(t, diff.Comparable)
	require.Len(t, diff.Calls, 1)

	call := diff.Calls[0]
	assert.InDelta(t, 100, call.MedianShiftPercent, 15, "a doubling reads as roughly +100%")
	assert.True(t, call.Significant)
	assert.Greater(t, call.CurrentWaiting, call.BaselineWaiting)
}

// Two runs that counted errors differently produce a diff that looks perfectly
// well-formed and answers no question.
func TestCompareRunsRefusesMismatchedErrorSemantics(t *testing.T) {
	baseManifest := manifest()
	currManifest := manifest()
	currManifest.ErrorRateSemantics = "http_errors_only"

	baseline, err := LoadRun(writeRun(t, baseManifest, drawSamples(t, "call", 50, time.Millisecond, 1)))
	require.NoError(t, err)
	current, err := LoadRun(writeRun(t, currManifest, drawSamples(t, "call", 50, time.Millisecond, 2)))
	require.NoError(t, err)

	diff := CompareRuns(baseline, current, ReportOptions{})
	assert.False(t, diff.Comparable)
	assert.Contains(t, diff.Refusal, "counted errors differently")
	assert.Empty(t, diff.Calls, "a refused comparison reports no figures")
}

func TestCompareRunsWarnsAboutSettingsThatChangeMeaning(t *testing.T) {
	baseManifest := manifest()
	currManifest := manifest()
	currManifest.Seed = 99
	currManifest.TargetRPS = 500
	currManifest.BatchSize = 10
	currManifest.Saturation = "drop"

	baseline, err := LoadRun(writeRun(t, baseManifest, drawSamples(t, "call", 50, time.Millisecond, 1)))
	require.NoError(t, err)
	current, err := LoadRun(writeRun(t, currManifest, drawSamples(t, "call", 50, time.Millisecond, 2)))
	require.NoError(t, err)

	diff := CompareRuns(baseline, current, ReportOptions{})
	require.True(t, diff.Comparable, "these are caveats, not a refusal")

	joined := ""
	for _, w := range diff.Warnings {
		joined += w + "\n"
	}
	assert.Contains(t, joined, "different seeds")
	assert.Contains(t, joined, "different target rates")
	assert.Contains(t, joined, "different batch sizes")
	assert.Contains(t, joined, "saturation policies")
}

func TestCompareRunsNamesCallsPresentInOnlyOneRun(t *testing.T) {
	baseline, err := LoadRun(writeRun(t, manifest(), drawSamples(t, "gone", 50, time.Millisecond, 1)))
	require.NoError(t, err)
	current, err := LoadRun(writeRun(t, manifest(), drawSamples(t, "added", 50, time.Millisecond, 2)))
	require.NoError(t, err)

	diff := CompareRuns(baseline, current, ReportOptions{})
	onlyIn := map[string]string{}
	for _, c := range diff.Calls {
		onlyIn[c.Name] = c.OnlyIn
	}
	assert.Equal(t, "baseline", onlyIn["gone"])
	assert.Equal(t, "current", onlyIn["added"])
}

// A handful of samples cannot support the normal approximation the test rests
// on, so the shift is reported and left unjudged.
func TestCompareRunsLeavesTinySamplesUnjudged(t *testing.T) {
	baseline, err := LoadRun(writeRun(t, manifest(), drawSamples(t, "call", 5, 10*time.Millisecond, 1)))
	require.NoError(t, err)
	current, err := LoadRun(writeRun(t, manifest(), drawSamples(t, "call", 5, 40*time.Millisecond, 2)))
	require.NoError(t, err)

	diff := CompareRuns(baseline, current, ReportOptions{})
	require.Len(t, diff.Calls, 1)
	assert.False(t, diff.Calls[0].Significant)
	assert.Zero(t, diff.Calls[0].PValue)
	assert.NotZero(t, diff.Calls[0].MedianShiftPercent, "the shift is still reported")
}

// Without a client filter the per-call figures pool every target's samples,
// which is a blend of two things rather than a measurement of either.
func TestCompareRunsWarnsWhenSeveralTargetsArePooled(t *testing.T) {
	twoClients := func(seed int64) string {
		samples := drawSamples(t, "call", 60, 10*time.Millisecond, seed)
		for i := range samples {
			if i%2 == 1 {
				samples[i].Client = "second"
			}
		}
		return writeRun(t, manifest(), samples)
	}

	baseline, err := LoadRun(twoClients(1))
	require.NoError(t, err)
	current, err := LoadRun(twoClients(2))
	require.NoError(t, err)

	pooled := CompareRuns(baseline, current, ReportOptions{})
	require.True(t, pooled.Comparable)
	joined := strings.Join(pooled.Warnings, "\n")
	assert.Contains(t, joined, "pools them")

	filtered := CompareRuns(baseline, current, ReportOptions{Client: "c"})
	assert.NotContains(t, strings.Join(filtered.Warnings, "\n"), "pools them",
		"naming a client resolves it, so there is nothing to warn about")
}

// A run that was killed, OOMed or filled the disk leaves a sample stream that
// stops mid-record. Every record before that point is still good, and a run
// that died is usually the one worth reading.
func TestLoadRunRecoversATruncatedSampleStream(t *testing.T) {
	dir := writeRun(t, manifest(), drawSamples(t, "call", 400, 10*time.Millisecond, 1))
	path := filepath.Join(dir, SampleFilename)

	whole, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, whole[:len(whole)*9/10], 0o600))

	run, err := LoadRun(dir)
	require.NoError(t, err, "a truncated tail must not cost the whole run")
	assert.True(t, run.Truncated, "and the report has to say so")
	assert.NotEmpty(t, run.Samples)
	assert.Less(t, len(run.Samples), 400)

	assert.EqualValues(t, len(run.Samples), run.Totals(ReportOptions{}).Count)
}

// A stream with no complete record at all is a different thing: there is
// nothing to report, so it stays an error.
func TestLoadRunStillFailsWhenNothingIsReadable(t *testing.T) {
	dir := writeRun(t, manifest(), drawSamples(t, "call", 5, time.Millisecond, 1))
	require.NoError(t, os.WriteFile(filepath.Join(dir, SampleFilename), []byte("not gzip"), 0o600))

	_, err := LoadRun(dir)
	require.Error(t, err)
}

func TestCompareRunsWarnsAboutATruncatedRun(t *testing.T) {
	good := writeRun(t, manifest(), drawSamples(t, "call", 100, 10*time.Millisecond, 1))
	cut := writeRun(t, manifest(), drawSamples(t, "call", 400, 10*time.Millisecond, 2))
	path := filepath.Join(cut, SampleFilename)
	whole, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, whole[:len(whole)*9/10], 0o600))

	baseline, err := LoadRun(good)
	require.NoError(t, err)
	current, err := LoadRun(cut)
	require.NoError(t, err)

	diff := CompareRuns(baseline, current, ReportOptions{})
	assert.Contains(t, strings.Join(diff.Warnings, "\n"), "ends mid-record")
}
