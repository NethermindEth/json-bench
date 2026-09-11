package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/jsonrpc-bench/runner/types"
)

// StoredRun is a completed run read back off disk.
type StoredRun struct {
	Dir      string
	Manifest types.RunManifest
	Samples  []Sample
}

// LoadRun reads a run's manifest and samples from an output directory. Retaining
// every sample is what makes this possible at all: the percentiles are
// recomputed from the observations rather than read back from a summary that
// already threw them away.
func LoadRun(dir string) (*StoredRun, error) {
	run := &StoredRun{Dir: dir}

	manifestPath := filepath.Join(dir, ManifestFilename)
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", manifestPath, err)
	}
	if err := json.Unmarshal(raw, &run.Manifest); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", manifestPath, err)
	}

	samplesPath := filepath.Join(dir, SampleFilename)
	file, err := os.Open(samplesPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s (a run written with --no-samples cannot be reported on): %w", samplesPath, err)
	}
	defer file.Close()

	run.Samples, err = ReadSamples(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", samplesPath, err)
	}
	if len(run.Samples) == 0 {
		return nil, fmt.Errorf("%s contains no samples", samplesPath)
	}
	return run, nil
}

// Clients lists the client names the run measured, in a stable order.
func (r *StoredRun) Clients() []string {
	seen := make(map[string]struct{})
	var out []string
	for _, s := range r.Samples {
		if _, dup := seen[s.Client]; dup {
			continue
		}
		seen[s.Client] = struct{}{}
		out = append(out, s.Client)
	}
	sort.Strings(out)
	return out
}

// CallStats is one call's recomputed distribution, including the phase split.
// The phases are the reason this exists rather than a summary table: a latency
// difference that sits entirely in `waiting` is the node thinking, and one that
// sits in `sending` or `receiving` is the generator or the link.
type CallStats struct {
	Name     string           `json:"name"`
	Method   string           `json:"method"`
	Count    int64            `json:"count"`
	Outcomes map[string]int64 `json:"outcomes"`
	Errors   int64            `json:"errors"`

	Duration  types.MetricSummary `json:"duration"`
	Waiting   types.MetricSummary `json:"waiting"`
	Sending   types.MetricSummary `json:"sending"`
	Receiving types.MetricSummary `json:"receiving"`
	Blocked   types.MetricSummary `json:"blocked"`

	durations []float64
}

// Durations exposes the retained observations so a comparison can run a
// distribution test rather than diff two summaries.
func (c *CallStats) Durations() []float64 { return c.durations }

// ReportOptions selects what a report covers.
type ReportOptions struct {
	Client  string // empty means every client
	Warmup  bool   // include warmup samples
	Outcome string // empty means every outcome; "ok" restricts to successes
}

// CallReport recomputes the per-call breakdown from retained samples.
func (r *StoredRun) CallReport(opts ReportOptions) []*CallStats {
	byCall := make(map[string]*CallStats)
	phases := make(map[string]map[Phase][]float64)

	for _, s := range r.Samples {
		if opts.Client != "" && s.Client != opts.Client {
			continue
		}
		if s.Warmup && !opts.Warmup {
			continue
		}
		if opts.Outcome != "" && string(s.Outcome) != opts.Outcome {
			continue
		}

		stats, ok := byCall[s.Name]
		if !ok {
			stats = &CallStats{Name: s.Name, Method: s.Method, Outcomes: map[string]int64{}}
			byCall[s.Name] = stats
			phases[s.Name] = map[Phase][]float64{}
		}
		stats.Count++
		stats.Outcomes[string(s.Outcome)]++
		if s.Outcome.IsError() {
			stats.Errors++
		}
		p := phases[s.Name]
		p[PhaseDuration] = append(p[PhaseDuration], msOf(s.Service()))
		p[PhaseWaiting] = append(p[PhaseWaiting], msOf(s.Phases.Waiting))
		p[PhaseSending] = append(p[PhaseSending], msOf(s.Phases.Sending))
		p[PhaseReceiving] = append(p[PhaseReceiving], msOf(s.Phases.Receiving))
		p[PhaseBlocked] = append(p[PhaseBlocked], msOf(s.Phases.Blocked))
	}

	out := make([]*CallStats, 0, len(byCall))
	for name, stats := range byCall {
		p := phases[name]
		stats.durations = p[PhaseDuration]
		stats.Duration = summarizeValues(p[PhaseDuration])
		stats.Waiting = summarizeValues(p[PhaseWaiting])
		stats.Sending = summarizeValues(p[PhaseSending])
		stats.Receiving = summarizeValues(p[PhaseReceiving])
		stats.Blocked = summarizeValues(p[PhaseBlocked])
		out = append(out, stats)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Totals recomputes the run-wide distribution across every call.
func (r *StoredRun) Totals(opts ReportOptions) *CallStats {
	total := &CallStats{Name: "(all)", Outcomes: map[string]int64{}}
	var durations, waiting, sending, receiving, blocked []float64
	for _, s := range r.Samples {
		if opts.Client != "" && s.Client != opts.Client {
			continue
		}
		if s.Warmup && !opts.Warmup {
			continue
		}
		if opts.Outcome != "" && string(s.Outcome) != opts.Outcome {
			continue
		}
		total.Count++
		total.Outcomes[string(s.Outcome)]++
		if s.Outcome.IsError() {
			total.Errors++
		}
		durations = append(durations, msOf(s.Service()))
		waiting = append(waiting, msOf(s.Phases.Waiting))
		sending = append(sending, msOf(s.Phases.Sending))
		receiving = append(receiving, msOf(s.Phases.Receiving))
		blocked = append(blocked, msOf(s.Phases.Blocked))
	}
	total.durations = durations
	total.Duration = summarizeValues(durations)
	total.Waiting = summarizeValues(waiting)
	total.Sending = summarizeValues(sending)
	total.Receiving = summarizeValues(receiving)
	total.Blocked = summarizeValues(blocked)
	return total
}
