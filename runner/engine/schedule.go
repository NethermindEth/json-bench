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

// schedule is the arrival plan for one client.
type schedule struct {
	total    int
	interval time.Duration
	duration time.Duration
}

// newSchedule derives the plan from the load config. A positive rps gives an
// open model with a fixed interval; iterations mode dispatches as fast as the
// pool allows, bounded by the duration.
func newSchedule(cfg *config.Config, available int) (schedule, error) {
	duration, err := time.ParseDuration(cfg.Duration)
	if err != nil {
		return schedule{}, fmt.Errorf("failed to parse config duration: %w", err)
	}

	if cfg.RPS > 0 {
		total := int(float64(cfg.RPS) * duration.Seconds())
		if total > available {
			total = available
		}
		return schedule{
			total:    total,
			interval: time.Duration(float64(time.Second) / float64(cfg.RPS)),
			duration: duration,
		}, nil
	}

	total := cfg.Iterations
	if total > available {
		total = available
	}
	return schedule{total: total, duration: duration}, nil
}

// due returns when request i should be sent, computed from the run's start
// rather than by accumulating sleeps, so ticker drift and generator GC pauses
// cannot push the whole plan later and hide themselves in the latency.
func (s schedule) due(start time.Time, i int) time.Time {
	if s.interval <= 0 {
		return time.Time{}
	}
	return start.Add(time.Duration(i) * s.interval)
}

// paced reports whether arrivals are on a fixed schedule.
func (s schedule) paced() bool { return s.interval > 0 }

// lateThreshold is how far behind schedule a request must go out to count as
// late: a full inter-arrival interval, so ordinary jitter is not reported as a
// delivery failure.
func (s schedule) lateThreshold() time.Duration {
	if s.interval <= 0 {
		return 0
	}
	return s.interval
}
