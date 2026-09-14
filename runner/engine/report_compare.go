package engine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jsonrpc-bench/runner/types"
)

// CallDiff is one call's change between two runs.
type CallDiff struct {
	Name   string `json:"name"`
	Method string `json:"method"`

	BaselineCount int64 `json:"baseline_count"`
	CurrentCount  int64 `json:"current_count"`

	BaselineP50 float64 `json:"baseline_p50_ms"`
	CurrentP50  float64 `json:"current_p50_ms"`
	BaselineP95 float64 `json:"baseline_p95_ms"`
	CurrentP95  float64 `json:"current_p95_ms"`
	BaselineP99 float64 `json:"baseline_p99_ms"`
	CurrentP99  float64 `json:"current_p99_ms"`

	// MedianShiftPercent is the change the distribution test is about. The
	// p-value says only that the shift is real, and at these sample sizes almost
	// any shift is, so the shift is the number worth reading.
	MedianShiftPercent float64 `json:"median_shift_percent"`
	PValue             float64 `json:"p_value"`
	Significant        bool    `json:"significant"`

	// Waiting is broken out because it answers a different question from the
	// total: a shift confined to waiting is the node, and one that shows up in
	// sending or receiving is the generator or the link.
	BaselineWaiting float64 `json:"baseline_waiting_avg_ms"`
	CurrentWaiting  float64 `json:"current_waiting_avg_ms"`

	BaselineErrors int64 `json:"baseline_errors"`
	CurrentErrors  int64 `json:"current_errors"`

	// OnlyIn names the run a call appears in when it appears in just one, which
	// means the two runs did not drive the same workload.
	OnlyIn string `json:"only_in,omitempty"`
}

// RunDiff is the comparison of two stored runs.
type RunDiff struct {
	Baseline string `json:"baseline"`
	Current  string `json:"current"`

	// Comparable is false when the two runs cannot be meaningfully diffed at
	// all; Warnings carries the softer caveats that still let a reader proceed.
	Comparable bool     `json:"comparable"`
	Refusal    string   `json:"refusal,omitempty"`
	Warnings   []string `json:"warnings,omitempty"`

	Totals CallDiff   `json:"totals"`
	Calls  []CallDiff `json:"calls"`
}

// CompareRuns diffs two stored runs call by call.
//
// It checks first whether the comparison means anything. Two runs that counted
// errors differently, or drove different requests, produce a diff that looks
// perfectly well-formed and answers no question — which is the failure mode the
// comparability gate exists to prevent.
func CompareRuns(baseline, current *StoredRun, opts ReportOptions) *RunDiff {
	diff := &RunDiff{
		Baseline:   baseline.Dir,
		Current:    current.Dir,
		Comparable: true,
	}

	if refusal := incomparable(baseline, current); refusal != "" {
		diff.Comparable = false
		diff.Refusal = refusal
		return diff
	}
	diff.Warnings = comparisonWarnings(baseline, current)
	for _, run := range []*StoredRun{baseline, current} {
		if run.Truncated {
			diff.Warnings = append(diff.Warnings, fmt.Sprintf(
				"%s ends mid-record and covers only the %d requests written before it stopped",
				run.Dir, len(run.Samples)))
		}
	}
	if opts.Client == "" {
		// Without a client filter the per-call figures pool every target's
		// samples into one distribution, which is a blend of two things rather
		// than a measurement of either.
		for _, run := range []*StoredRun{baseline, current} {
			if names := run.Clients(); len(names) > 1 {
				diff.Warnings = append(diff.Warnings, fmt.Sprintf(
					"%s measured %d targets (%s) and no --client was given, so every figure below "+
						"pools them", run.Dir, len(names), strings.Join(names, ", ")))
			}
		}
	}

	base := indexCalls(baseline.CallReport(opts))
	curr := indexCalls(current.CallReport(opts))

	names := make([]string, 0, len(base)+len(curr))
	seen := map[string]struct{}{}
	for name := range base {
		names = append(names, name)
		seen[name] = struct{}{}
	}
	for name := range curr {
		if _, dup := seen[name]; !dup {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	for _, name := range names {
		diff.Calls = append(diff.Calls, diffCall(name, base[name], curr[name]))
	}
	diff.Totals = diffCall("(all)", baseline.Totals(opts), current.Totals(opts))
	return diff
}

func indexCalls(stats []*CallStats) map[string]*CallStats {
	out := make(map[string]*CallStats, len(stats))
	for _, s := range stats {
		out[s.Name] = s
	}
	return out
}

// incomparable reports why two runs must not be diffed, or "" when they may be.
func incomparable(baseline, current *StoredRun) string {
	a, b := baseline.Manifest.ErrorRateSemantics, current.Manifest.ErrorRateSemantics
	if a != "" && b != "" && a != b {
		return fmt.Sprintf("the runs counted errors differently (%s vs %s), so their error rates and "+
			"every derived figure are not the same measurement", a, b)
	}
	return ""
}

// comparisonWarnings lists the ways two runs differ that change what the diff
// means without making it meaningless.
func comparisonWarnings(baseline, current *StoredRun) []string {
	var out []string
	bm, cm := baseline.Manifest, current.Manifest

	if bm.Seed != cm.Seed {
		out = append(out, fmt.Sprintf("different seeds (%d vs %d): the runs drove different requests, "+
			"so a latency difference may be a workload difference", bm.Seed, cm.Seed))
	}
	if bm.TestName != cm.TestName {
		out = append(out, fmt.Sprintf("different test names (%q vs %q)", bm.TestName, cm.TestName))
	}
	if bm.TargetRPS != cm.TargetRPS {
		out = append(out, fmt.Sprintf("different target rates (%d vs %d rps): latency under different "+
			"offered load is not comparable", bm.TargetRPS, cm.TargetRPS))
	}
	if bm.BatchSize != cm.BatchSize {
		out = append(out, fmt.Sprintf("different batch sizes (%d vs %d): a batched request records the "+
			"whole round trip's latency, so per-call figures mean different things", bm.BatchSize, cm.BatchSize))
	}
	if bm.Saturation != cm.Saturation {
		out = append(out, fmt.Sprintf("different saturation policies (%s vs %s): one run may have shed "+
			"load the other queued", bm.Saturation, cm.Saturation))
	}
	if bm.Concurrency != cm.Concurrency {
		out = append(out, fmt.Sprintf("different concurrency (%d vs %d)", bm.Concurrency, cm.Concurrency))
	}
	if bm.AcceptCompression != cm.AcceptCompression || bm.HTTP2 != cm.HTTP2 || bm.ReuseConnections != cm.ReuseConnections {
		out = append(out, "different transport settings (compression, HTTP/2 or connection reuse)")
	}
	if versionsDiffer(bm, cm) {
		out = append(out, "the targets reported different client versions, which is usually the point "+
			"of the comparison but means node behaviour is not held constant")
	}
	return out
}

func versionsDiffer(a, b types.RunManifest) bool {
	versions := func(m types.RunManifest) map[string]string {
		out := map[string]string{}
		for _, c := range m.Clients {
			out[c.Name] = c.ClientVersion
		}
		return out
	}
	av, bv := versions(a), versions(b)
	for name, version := range av {
		if other, ok := bv[name]; ok && other != version {
			return true
		}
	}
	return false
}

func diffCall(name string, baseline, current *CallStats) CallDiff {
	d := CallDiff{Name: name}
	switch {
	case baseline == nil:
		d.OnlyIn = "current"
	case current == nil:
		d.OnlyIn = "baseline"
	}

	if baseline != nil {
		d.Method = baseline.Method
		d.BaselineCount = baseline.Count
		d.BaselineP50, d.BaselineP95, d.BaselineP99 = baseline.Duration.P50, baseline.Duration.P95, baseline.Duration.P99
		d.BaselineWaiting = baseline.Waiting.Avg
		d.BaselineErrors = baseline.Errors
	}
	if current != nil {
		d.Method = current.Method
		d.CurrentCount = current.Count
		d.CurrentP50, d.CurrentP95, d.CurrentP99 = current.Duration.P50, current.Duration.P95, current.Duration.P99
		d.CurrentWaiting = current.Waiting.Avg
		d.CurrentErrors = current.Errors
	}

	if baseline == nil || current == nil {
		return d
	}
	// Below this the normal approximation behind the test is not trustworthy, so
	// the shift is reported and left unjudged rather than given a p-value that
	// reads as authoritative.
	if len(baseline.Durations()) < minComparisonSamples || len(current.Durations()) < minComparisonSamples {
		if d.BaselineP50 > 0 {
			d.MedianShiftPercent = (d.CurrentP50 - d.BaselineP50) / d.BaselineP50 * 100
		}
		return d
	}

	comparison := compareSamples(name, "baseline", "current", baseline.Durations(), current.Durations())
	d.MedianShiftPercent = comparison.MedianShiftPercent
	d.PValue = comparison.PValue
	// Faster is set only when the test actually ran and resolved a direction,
	// which is how a comparison too small to judge stays unjudged.
	d.Significant = comparison.Faster != ""
	return d
}
