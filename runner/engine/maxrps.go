package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/types"
)

// SLO is the service level a probe must hold for its rate to count as
// sustainable. A search without one would just find the rate at which the
// endpoint stops answering, which is not the same question.
type SLO struct {
	// P99Ms caps the 99th percentile latency, in milliseconds.
	P99Ms float64

	// ErrorRatePercent caps the share of requests that failed, counting
	// JSON-RPC errors.
	ErrorRatePercent float64

	// MinDeliveredPercent is how much of the offered load must actually have
	// been sent for a probe to be conclusive at all.
	MinDeliveredPercent float64
}

func DefaultSLO() SLO {
	return SLO{P99Ms: 1000, ErrorRatePercent: 1, MinDeliveredPercent: 99}
}

func (s SLO) validate() error {
	if s.P99Ms <= 0 {
		return fmt.Errorf("--slo-p99 must be positive")
	}
	if s.ErrorRatePercent < 0 || s.ErrorRatePercent > 100 {
		return fmt.Errorf("--slo-error-rate must be a percentage")
	}
	if s.MinDeliveredPercent <= 0 || s.MinDeliveredPercent > 100 {
		return fmt.Errorf("--slo-min-delivered must be a percentage above zero")
	}
	return nil
}

// SearchOptions bounds the search.
type SearchOptions struct {
	SLO SLO

	MinRPS int
	MaxRPS int

	// ProbeDuration is how long each rate is held. Short probes find a number
	// quickly and measure warm-up as much as steady state; the default errs
	// towards being long enough to mean something.
	ProbeDuration time.Duration

	// Tolerance stops the search once the bracket between the highest passing
	// and lowest failing rate is this narrow, in requests per second.
	Tolerance int

	// Settle is a pause between probes, so a node has a chance to return to
	// rest and the next probe does not inherit the previous one's queue.
	Settle time.Duration
}

func DefaultSearchOptions() SearchOptions {
	return SearchOptions{
		SLO:           DefaultSLO(),
		MinRPS:        10,
		ProbeDuration: 30 * time.Second,
		Tolerance:     10,
		Settle:        5 * time.Second,
	}
}

// ProbeVerdict is what one probe established.
type ProbeVerdict string

const (
	// VerdictPass means the rate was offered in full and the SLO held.
	VerdictPass ProbeVerdict = "pass"

	// VerdictFail means the rate was offered in full and the SLO did not hold.
	VerdictFail ProbeVerdict = "fail"

	// VerdictGeneratorLimited means the generator could not offer the rate, so
	// the probe says nothing about the endpoint. Without delivery accounting
	// this case is invisible, and a search silently reports the generator's
	// ceiling as the node's capacity.
	VerdictGeneratorLimited ProbeVerdict = "generator_limited"
)

// Probe is one rate that was tried and what came of it.
type Probe struct {
	RPS           int          `json:"rps"`
	Verdict       ProbeVerdict `json:"verdict"`
	Reason        string       `json:"reason,omitempty"`
	DeliveredPct  float64      `json:"delivered_percent"`
	AchievedRPS   float64      `json:"achieved_rps"`
	P99Ms         float64      `json:"p99_ms"`
	ErrorRatePct  float64      `json:"error_rate_percent"`
	MaxDispatchMs float64      `json:"max_dispatch_delay_ms"`
	Requests      int64        `json:"requests"`
}

// SearchResult is the whole search: every probe in order, and the answer.
type SearchResult struct {
	Client string `json:"client"`
	SLO    struct {
		P99Ms               float64 `json:"p99_ms"`
		ErrorRatePercent    float64 `json:"error_rate_percent"`
		MinDeliveredPercent float64 `json:"min_delivered_percent"`
	} `json:"slo"`

	ProbeDurationSeconds float64 `json:"probe_duration_seconds"`
	Concurrency          int     `json:"concurrency"`

	// MaxRPS is the highest rate that held the SLO. Zero means even the lowest
	// rate tried did not.
	MaxRPS int `json:"max_rps"`

	// LowestFailingRPS brackets the answer from above. Zero means the search
	// never found a failing rate, so MaxRPS is a floor rather than a limit.
	LowestFailingRPS int `json:"lowest_failing_rps,omitempty"`

	// LimitedBy names what stopped the search: "slo", "search_ceiling" or
	// "generator". A generator limit is not a result about the endpoint.
	LimitedBy string `json:"limited_by"`

	Probes []Probe `json:"probes"`

	// Manifest records what was measured and how. Its target_rps is absent
	// because a search has no single rate, and its duration is one probe's.
	Manifest types.RunManifest `json:"manifest"`
}

// Target is what the search measured, when the targets were identified.
func (r SearchResult) Target() (types.ClientProvenance, bool) {
	if len(r.Manifest.Clients) == 1 {
		return r.Manifest.Clients[0], true
	}
	return types.ClientProvenance{}, false
}

// Conclusive reports whether the search measured the endpoint rather than the
// generator.
func (r SearchResult) Conclusive() bool { return r.LimitedBy != "generator" }

// FindMaxRPS searches for the highest request rate a single target sustains
// within the SLO.
//
// It ramps by doubling until the SLO breaks or the ceiling is reached, then
// bisects the bracket. Each probe is a complete run at that rate under the drop
// policy, so a rate the generator cannot offer is reported as such instead of
// being absorbed into the latency: measuring the generator's ceiling and
// calling it the node's capacity is the failure this is built to avoid.
func FindMaxRPS(ctx context.Context, cfg *config.Config, opts Options, search SearchOptions) (*SearchResult, error) {
	if len(cfg.ResolvedClients) != 1 {
		return nil, fmt.Errorf("the rate search measures one target at a time; %d are configured", len(cfg.ResolvedClients))
	}
	if err := search.SLO.validate(); err != nil {
		return nil, err
	}
	if search.MinRPS <= 0 {
		search.MinRPS = 1
	}
	if search.ProbeDuration <= 0 {
		search.ProbeDuration = DefaultSearchOptions().ProbeDuration
	}
	if search.Tolerance <= 0 {
		search.Tolerance = 1
	}
	// A ceiling below the floor is never probed, so the search would report a
	// limit it never tested against a bound it never respected.
	if search.MaxRPS > 0 && search.MaxRPS < search.MinRPS {
		return nil, fmt.Errorf("the rate ceiling (%d) is below the floor (%d)", search.MaxRPS, search.MinRPS)
	}

	log := opts.Logger
	if log == nil {
		log = logrus.New()
	}

	result := &SearchResult{
		Client:               cfg.ResolvedClients[0].Name,
		ProbeDurationSeconds: search.ProbeDuration.Seconds(),
		Concurrency:          cfg.VUs,
	}
	result.SLO.P99Ms = search.SLO.P99Ms
	result.SLO.ErrorRatePercent = search.SLO.ErrorRatePercent
	result.SLO.MinDeliveredPercent = search.SLO.MinDeliveredPercent

	start := time.Now()
	provenance, err := IdentifyTargets(ctx, cfg, opts)
	if err != nil {
		return nil, err
	}

	// The manifest is finalised on every return path, so a search that stopped
	// early still records what it was measuring.
	finish := func() {
		searchCfg := *cfg
		searchCfg.RPS = 0
		searchCfg.Iterations = 0
		searchCfg.Stages = nil
		searchCfg.Duration = search.ProbeDuration.String()
		probeOpts := opts
		probeOpts.Saturation = SaturationDrop
		result.Manifest = buildManifest(&searchCfg, probeOpts, provenance, start, time.Now())
	}
	defer finish()

	runProbe := func(rps int) (Probe, error) {
		probeCfg := *cfg
		probeCfg.RPS = rps
		probeCfg.Iterations = 0
		probeCfg.Stages = nil
		probeCfg.Duration = search.ProbeDuration.String()

		probeOpts := opts
		// Dropping rather than queueing keeps a probe inside its window and
		// makes a shortfall countable instead of turning into dispatch delay.
		probeOpts.Saturation = SaturationDrop
		probeOpts.WriteSamples = false
		probeOpts.OutputDir = ""
		probeOpts.Sink = nil
		// The targets were identified once, before the search started.
		probeOpts.SkipPreflight = true

		runResult, _, err := Run(ctx, &probeCfg, probeOpts)
		if err != nil {
			return Probe{}, err
		}
		probe := evaluateProbe(rps, runResult.ClientMetrics[result.Client], search.SLO)
		result.Probes = append(result.Probes, probe)
		log.WithFields(logrus.Fields{
			"rps":       rps,
			"verdict":   probe.Verdict,
			"delivered": fmt.Sprintf("%.1f%%", probe.DeliveredPct),
			"p99_ms":    fmt.Sprintf("%.1f", probe.P99Ms),
			"errors":    fmt.Sprintf("%.2f%%", probe.ErrorRatePct),
		}).Infof("probe at %d rps: %s", rps, probe.summary())
		return probe, nil
	}

	settle := func() bool {
		if search.Settle <= 0 {
			return true
		}
		timer := time.NewTimer(search.Settle)
		defer timer.Stop()
		select {
		case <-timer.C:
			return true
		case <-ctx.Done():
			return false
		}
	}

	// Ramp: double until something gives.
	var highestPass, lowestFail int
	rate := search.MinRPS
	for {
		probe, err := runProbe(rate)
		if err != nil {
			return result, err
		}

		switch probe.Verdict {
		case VerdictGeneratorLimited:
			result.MaxRPS = highestPass
			result.LimitedBy = "generator"
			return result, nil
		case VerdictFail:
			lowestFail = rate
		case VerdictPass:
			highestPass = rate
		}

		if lowestFail > 0 {
			break
		}
		if search.MaxRPS > 0 && rate >= search.MaxRPS {
			result.MaxRPS = highestPass
			result.LimitedBy = "search_ceiling"
			return result, nil
		}

		next := rate * 2
		if search.MaxRPS > 0 && next > search.MaxRPS {
			next = search.MaxRPS
		}
		rate = next

		if !settle() {
			result.MaxRPS = highestPass
			result.LimitedBy = "cancelled"
			return result, ctx.Err()
		}
	}

	// Bisect the bracket between the highest pass and the lowest failure.
	for lowestFail-highestPass > search.Tolerance {
		if !settle() {
			break
		}

		mid := highestPass + (lowestFail-highestPass)/2
		if mid <= highestPass || mid >= lowestFail {
			break
		}

		probe, err := runProbe(mid)
		if err != nil {
			return result, err
		}
		switch probe.Verdict {
		case VerdictGeneratorLimited:
			result.MaxRPS = highestPass
			result.LowestFailingRPS = lowestFail
			result.LimitedBy = "generator"
			return result, nil
		case VerdictPass:
			highestPass = mid
		default:
			lowestFail = mid
		}
	}

	result.MaxRPS = highestPass
	result.LowestFailingRPS = lowestFail
	result.LimitedBy = "slo"
	return result, ctx.Err()
}

// evaluateProbe decides a probe's verdict. Delivery is checked first: a rate
// the generator could not offer says nothing about the endpoint, whatever the
// latency looked like.
func evaluateProbe(rps int, client *types.ClientMetrics, slo SLO) Probe {
	if client == nil || client.TotalRequests == 0 {
		return Probe{RPS: rps, Verdict: VerdictGeneratorLimited, Reason: "no requests were sent"}
	}

	d := client.Delivery
	probe := Probe{
		RPS:           rps,
		DeliveredPct:  d.DeliveryRatio() * 100,
		AchievedRPS:   d.AchievedRPS,
		P99Ms:         client.Latency.P99,
		ErrorRatePct:  client.ErrorRate,
		MaxDispatchMs: d.MaxDispatchDelayMs,
		Requests:      client.TotalRequests,
	}

	if probe.DeliveredPct < slo.MinDeliveredPercent {
		probe.Verdict = VerdictGeneratorLimited
		probe.Reason = fmt.Sprintf("only %.1f%% of the offered load was sent (vus may be too low)", probe.DeliveredPct)
		return probe
	}

	switch {
	case probe.ErrorRatePct > slo.ErrorRatePercent:
		probe.Verdict = VerdictFail
		probe.Reason = fmt.Sprintf("error rate %.2f%% exceeds %.2f%%", probe.ErrorRatePct, slo.ErrorRatePercent)
	case probe.P99Ms > slo.P99Ms:
		probe.Verdict = VerdictFail
		probe.Reason = fmt.Sprintf("p99 %.1fms exceeds %.1fms", probe.P99Ms, slo.P99Ms)
	default:
		probe.Verdict = VerdictPass
	}
	return probe
}

func (p Probe) summary() string {
	if p.Reason != "" {
		return string(p.Verdict) + " — " + p.Reason
	}
	return fmt.Sprintf("%s — p99 %.1fms, errors %.2f%%", p.Verdict, p.P99Ms, p.ErrorRatePct)
}

// Explain states the answer and, as importantly, what bounded it.
func (r SearchResult) Explain() string {
	switch r.LimitedBy {
	case "generator":
		if r.MaxRPS == 0 {
			return "inconclusive: the generator could not offer even the lowest rate tried. Raise vus."
		}
		return fmt.Sprintf("at least %d rps, but the search stopped because the generator could not offer the next rate. "+
			"Raise vus to find the endpoint's limit; this is the generator's.", r.MaxRPS)
	case "search_ceiling":
		return fmt.Sprintf("at least %d rps, which was the search ceiling — the endpoint never breached the SLO, "+
			"so this is a floor rather than a limit.", r.MaxRPS)
	case "cancelled":
		return fmt.Sprintf("at least %d rps before the search was cancelled.", r.MaxRPS)
	default:
		if r.MaxRPS == 0 {
			return fmt.Sprintf("below %d rps: the lowest rate tried already breached the SLO.", r.LowestFailingRPS)
		}
		return fmt.Sprintf("%d rps sustained the SLO; %d rps did not.", r.MaxRPS, r.LowestFailingRPS)
	}
}
