package engine

import (
	"fmt"
	"time"

	"github.com/jsonrpc-bench/runner/config"
)

// Saturation is what the engine does when the in-flight limit is reached at the
// moment a request was due.
type Saturation string

const (
	// SaturationQueue dispatches the request as soon as a slot frees and records
	// how late it went out. The delay is the coordinated-omission error: without
	// it a saturated run reports the latency of the requests it managed to send
	// and looks fast.
	SaturationQueue Saturation = "queue"

	// SaturationDrop discards the request and counts it, which is what k6's
	// arrival-rate executor does. Kept for runs that must be comparable to k6.
	SaturationDrop Saturation = "drop"

	// SaturationAbort ends the run, so a CI job cannot publish numbers from a
	// generator that could not offer the requested load.
	SaturationAbort Saturation = "abort"
)

func ParseSaturation(s string) (Saturation, error) {
	switch Saturation(s) {
	case SaturationQueue, SaturationDrop, SaturationAbort:
		return Saturation(s), nil
	default:
		return "", fmt.Errorf("unknown saturation policy %q (want queue, drop or abort)", s)
	}
}

// rampStep is how finely the rate curve is integrated when placing arrivals.
// One millisecond is far below any rate worth benchmarking and keeps the
// placement exact enough that the delivered rate matches the configured one.
const rampStep = time.Millisecond

// schedule is the arrival plan for one client.
//
// A constant rate keeps its closed-form interval: the arithmetic is what
// committed request corpora were generated against, so it is left alone. A ramp
// precomputes its arrival offsets instead, by integrating the rate curve, which
// handles a linear ramp and a staircase with one implementation.
type schedule struct {
	total    int
	interval time.Duration
	duration time.Duration

	// offsets holds each request's time from the run's start when the rate is
	// not constant. Empty for a constant rate.
	offsets []time.Duration

	// warmup is how long the run applies load before the measured window
	// opens. Requests due before it are issued and then excluded from every
	// reported statistic.
	warmup time.Duration
}

// newSchedule derives the plan from the load config. A positive rps gives an
// open model with a fixed interval; stages ramp that rate over time; iterations
// mode dispatches as fast as the pool allows, bounded by the duration.
// newSchedule takes the number of arrivals available and how many requests each
// carries. Batching does not change the offered request rate: `rps` stays a
// rate of requests, so batches go out at rps/batchSize and two runs at
// different batch sizes offer the node the same work.
func newSchedule(cfg *config.Config, available, batchSize int) (schedule, error) {
	if batchSize < 1 {
		batchSize = 1
	}
	warmup, err := parseOptionalDuration(cfg.Warmup)
	if err != nil {
		return schedule{}, fmt.Errorf("failed to parse warmup: %w", err)
	}

	if len(cfg.Stages) > 0 {
		return newRampSchedule(cfg, warmup, available, batchSize)
	}

	duration, err := time.ParseDuration(cfg.Duration)
	if err != nil {
		return schedule{}, fmt.Errorf("failed to parse config duration: %w", err)
	}
	if warmup >= duration {
		return schedule{}, fmt.Errorf("warmup (%s) leaves no measured window inside the duration (%s)", warmup, duration)
	}

	if cfg.RPS > 0 {
		arrivalRate := float64(cfg.RPS) / float64(batchSize)
		total := int(arrivalRate * duration.Seconds())
		if total > available {
			total = available
		}
		return schedule{
			total:    total,
			interval: time.Duration(float64(time.Second) / arrivalRate),
			duration: duration,
			warmup:   warmup,
		}, nil
	}

	total := cfg.Iterations
	if total > available {
		total = available
	}
	return schedule{total: total, duration: duration}, nil
}

// newRampSchedule places arrivals under a piecewise-linear rate curve.
func newRampSchedule(cfg *config.Config, warmup time.Duration, available, batchSize int) (schedule, error) {
	offsets, duration, err := RampOffsets(cfg.RPS, cfg.Stages, batchSize)
	if err != nil {
		return schedule{}, err
	}
	if warmup >= duration {
		return schedule{}, fmt.Errorf("warmup (%s) leaves no measured window inside the staged run (%s)", warmup, duration)
	}
	if len(offsets) > available {
		offsets = offsets[:available]
	}
	return schedule{
		total:    len(offsets),
		duration: duration,
		offsets:  offsets,
		warmup:   warmup,
	}, nil
}

// RampOffsets integrates the rate curve described by startRPS and the stages,
// returning each request's offset from the run's start and the total length.
//
// Within a stage the rate moves linearly, so the requests due in a slice of
// time are the area under that line. Accumulating the area and emitting an
// arrival whenever a whole request has built up places them at the right
// density without needing to invert the curve.
// batchSize divides the rate, since one arrival carries that many requests.
func RampOffsets(startRPS int, stages []config.Stage, batchSize int) ([]time.Duration, time.Duration, error) {
	if batchSize < 1 {
		batchSize = 1
	}
	rate := float64(startRPS) / float64(batchSize)
	var elapsed time.Duration
	var pending float64
	var offsets []time.Duration

	for i, stage := range stages {
		stageDuration, err := time.ParseDuration(stage.Duration)
		if err != nil {
			return nil, 0, fmt.Errorf("stage %d has an invalid duration %q: %w", i+1, stage.Duration, err)
		}

		target := float64(stage.Target) / float64(batchSize)
		steps := int(stageDuration / rampStep)
		if steps < 1 {
			steps = 1
		}

		for step := 0; step < steps; step++ {
			// The rate at the midpoint of the slice, which is the average over
			// it for a linear ramp.
			progress := (float64(step) + 0.5) / float64(steps)
			current := rate + (target-rate)*progress
			pending += current * rampStep.Seconds()

			for pending >= 1 {
				pending--
				offsets = append(offsets, elapsed+time.Duration(step)*rampStep)
			}
		}

		elapsed += stageDuration
		rate = target
	}

	return offsets, elapsed, nil
}

func parseOptionalDuration(v string) (time.Duration, error) {
	if v == "" {
		return 0, nil
	}
	return time.ParseDuration(v)
}

// due returns when request i should be sent, computed from the run's start
// rather than by accumulating sleeps, so ticker drift and generator GC pauses
// cannot push the whole plan later and hide themselves in the latency.
func (s schedule) due(start time.Time, i int) time.Time {
	if len(s.offsets) > 0 {
		if i >= len(s.offsets) {
			return time.Time{}
		}
		return start.Add(s.offsets[i])
	}
	if s.interval <= 0 {
		return time.Time{}
	}
	return start.Add(time.Duration(i) * s.interval)
}

// paced reports whether arrivals are on a fixed schedule.
func (s schedule) paced() bool { return s.interval > 0 || len(s.offsets) > 0 }

// measureFrom is when the measured window opens. Requests due before it are
// warmup and are excluded from every reported statistic.
func (s schedule) measureFrom(start time.Time) time.Time { return start.Add(s.warmup) }

// isWarmup reports whether a request due at this moment falls in the warmup.
func (s schedule) isWarmup(start, due time.Time) bool {
	return s.warmup > 0 && due.Before(s.measureFrom(start))
}

// lateThreshold is how far behind schedule a request must go out to count as
// late: a full inter-arrival interval, so ordinary jitter is not reported as a
// delivery failure. Under a ramp the mean interval stands in, since the
// instantaneous one changes throughout.
func (s schedule) lateThreshold() time.Duration {
	if s.interval > 0 {
		return s.interval
	}
	if len(s.offsets) > 1 && s.duration > 0 {
		return s.duration / time.Duration(len(s.offsets))
	}
	return 0
}
