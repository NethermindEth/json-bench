package review

import (
	"fmt"
	"math"
	"sort"
)

// Dist summarises a sample of milliseconds with nearest-rank percentiles.
type Dist struct {
	N   int     `json:"n"`
	Min float64 `json:"min_ms"`
	P50 float64 `json:"p50_ms"`
	P95 float64 `json:"p95_ms"`
	P99 float64 `json:"p99_ms"`
	Max float64 `json:"max_ms"`
}

func newDist(v []float64) *Dist {
	if len(v) == 0 {
		return nil
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	pct := func(p float64) float64 {
		rank := int(math.Ceil(p/100*float64(len(s)))) - 1
		if rank < 0 {
			rank = 0
		}
		return round3(s[rank])
	}
	return &Dist{N: len(s), Min: round3(s[0]), P50: pct(50), P95: pct(95), P99: pct(99), Max: round3(s[len(s)-1])}
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

// PairStats covers one pair and one probe. Status counts every block in the
// pair's range; the denominator is the blocks it was measured on (success or
// failure), so unmeasurable blocks (orphaned, unverified, late-armed, empty)
// stay visible without diluting availability.
type PairStats struct {
	Denominator  int                 `json:"denominator"`
	Status       map[string]int      `json:"status"`
	Freshness    *Dist               `json:"freshness,omitempty"`
	Availability map[string]float64  `json:"availability_by_deadline"`
	LeftCensored int                 `json:"left_censored"`
	OutsideSlot  int                 `json:"matched_outside_slot"`
	LocalWrong   int                 `json:"local_verdict_disagreed"`
	SkippedPolls int                 `json:"skipped_polls"`
	Attempts     int                 `json:"attempts"`
	CL           map[string]*CLSplit `json:"cl_split,omitempty"`
}

func pairStats(outs []*Outcome, deadlines []float64, slotMs float64) *PairStats {
	st := &PairStats{Status: map[string]int{}, Availability: map[string]float64{}}
	var fresh []float64
	for _, o := range outs {
		st.Status[o.Status]++
		st.Attempts += o.Attempts
		st.SkippedPolls += o.SkippedPolls
		if o.LocalDisagrees {
			st.LocalWrong++
		}
		if !o.Comparable() && o.Status != OutWrongNA {
			continue
		}
		st.Denominator++
		if o.Status != OutMatched {
			continue
		}
		fresh = append(fresh, *o.FreshnessMs)
		if o.LeftCensored {
			st.LeftCensored++
		}
		if o.WithinSlot != nil && !*o.WithinSlot {
			st.OutsideSlot++
		}
	}
	st.Freshness = newDist(fresh)
	all := append(append([]float64(nil), deadlines...), slotMs)
	for i, d := range all {
		n := 0
		for _, f := range fresh {
			if f <= d {
				n++
			}
		}
		key := fmt.Sprintf("%gms", d)
		if i == len(all)-1 {
			key = "slot"
		}
		if st.Denominator > 0 {
			st.Availability[key] = round3(float64(n) / float64(st.Denominator))
		} else {
			st.Availability[key] = 0
		}
	}
	return st
}

// Comparison is a pairwise freshness comparison of A against B for one
// probe. Observed counts compare first correct responses directly; inferred
// counts use availability intervals and call overlaps unresolved.
type Comparison struct {
	Group         string                    `json:"group"`
	A             string                    `json:"a"`
	B             string                    `json:"b"`
	MarginMs      float64                   `json:"effective_margin_ms"`
	CrossHost     bool                      `json:"cross_host"`
	Compared      int                       `json:"compared"`
	ObservedAWins int                       `json:"observed_a_wins"`
	ObservedBWins int                       `json:"observed_b_wins"`
	ObservedTies  int                       `json:"observed_ties"`
	InferredAWins int                       `json:"inferred_a_wins"`
	InferredBWins int                       `json:"inferred_b_wins"`
	Unresolved    int                       `json:"inferred_unresolved"`
	CoverageAWins int                       `json:"coverage_a_wins"`
	CoverageBWins int                       `json:"coverage_b_wins"`
	BothFailed    int                       `json:"both_failed"`
	Excluded      map[string]int            `json:"excluded"`
	Delta         *Dist                     `json:"delta_a_minus_b,omitempty"`
	AWinMargin    *Dist                     `json:"a_win_margin,omitempty"`
	BWinMargin    *Dist                     `json:"b_win_margin,omitempty"`
	CL            map[string]*PairedCLSplit `json:"cl_split,omitempty"`
}

type side struct {
	run *ProbeRun
	out *Outcome
}

// compare accumulates one block into c.
func (c *Comparison) add(a, b side, margin float64, deltas, aw, bw *[]float64) {
	oa, ob := a.out, b.out
	if oa == nil || ob == nil {
		c.Excluded["not_in_both"]++
		return
	}
	if !oa.Comparable() || !ob.Comparable() {
		reason := oa.Status
		if oa.Comparable() {
			reason = ob.Status
		}
		c.Excluded[reason]++
		return
	}
	if c.CrossHost && (oa.ClockStep || ob.ClockStep) {
		c.Excluded["clock_step"]++
		return
	}
	c.Compared++
	am, bm := oa.Status == OutMatched, ob.Status == OutMatched
	switch {
	case !am && !bm:
		c.BothFailed++
		return
	case am && !bm:
		c.CoverageAWins++
		return
	case !am && bm:
		c.CoverageBWins++
		return
	}
	delta := *oa.FreshnessMs - *ob.FreshnessMs
	*deltas = append(*deltas, delta)
	switch {
	case math.Abs(delta) < margin:
		c.ObservedTies++
	case delta < 0:
		c.ObservedAWins++
		*aw = append(*aw, -delta)
	default:
		c.ObservedBWins++
		*bw = append(*bw, delta)
	}

	var errA, errB float64
	if c.CrossHost {
		errA, errB = a.run.ClockErrorMs()*1e6, b.run.ClockErrorMs()*1e6
	}
	loA, hiA := bounds(oa, errA)
	loB, hiB := bounds(ob, errB)
	switch {
	case hiA < loB:
		c.InferredAWins++
	case hiB < loA:
		c.InferredBWins++
	default:
		c.Unresolved++
	}
}

// bounds returns the availability interval in wall nanoseconds. Without an
// explicit earlier "not ready" the lower end is unbounded (left-censored).
func bounds(o *Outcome, errNs float64) (float64, float64) {
	hi := float64(o.readyNs) + errNs
	if !o.hasLower || o.LeftCensored {
		return math.Inf(-1), hi
	}
	return float64(o.lowerNs) - errNs, hi
}
