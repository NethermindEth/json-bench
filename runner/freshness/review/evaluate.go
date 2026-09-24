package review

import (
	"math"
	"strconv"

	"github.com/jsonrpc-bench/runner/freshness/schema"
)

// Review statuses for one probe of one pair on one block identity.
const (
	OutMatched        = "matched"
	OutNotApplicable  = "not_applicable"
	OutWrongNA        = "incorrect_not_applicable"
	OutIncorrect      = "incorrect"
	OutTimeout        = "timeout"
	OutAborted        = "aborted"
	OutLateArmed      = "late_armed"
	OutUnsupported    = "unsupported"
	OutUnverified     = "unverified"
	OutOrphaned       = "orphaned"
	OutDifferentBlock = "different_block"
	OutNotObserved    = "not_observed"
	OutOutOfRange     = "out_of_range"
	OutWarmup         = "warmup"
)

// Outcome is the verified result of one probe. Times are milliseconds from
// the block's slot start; bounds are before clock-error widening, which
// depends on which pair it is compared against.
type Outcome struct {
	Status         string   `json:"status"`
	Reason         string   `json:"reason,omitempty"`
	ReadyWall      *string  `json:"t_ready_wall_ns,omitempty"`
	FreshnessMs    *float64 `json:"freshness_ms,omitempty"`
	LowerMs        *float64 `json:"lower_bound_ms,omitempty"`
	FirstSentMs    *float64 `json:"first_sent_ms,omitempty"`
	LeftCensored   bool     `json:"left_censored,omitempty"`
	WithinSlot     *bool    `json:"within_slot,omitempty"`
	Attempts       int      `json:"attempts"`
	SkippedPolls   int      `json:"skipped_polls"`
	ProbeStatus    string   `json:"probe_status,omitempty"`
	LocalDisagrees bool     `json:"local_disagrees,omitempty"`
	OrderViolation bool     `json:"order_violation,omitempty"`
	ClockStep      bool     `json:"clock_step,omitempty"`
	WeakLowerBound bool     `json:"weak_lower_bound,omitempty"`
	readyNs        int64
	lowerNs        int64
	hasLower       bool
}

// Comparable reports whether the outcome takes part in pairwise comparison
// (as a success or as a failure).
func (o *Outcome) Comparable() bool {
	switch o.Status {
	case OutMatched, OutIncorrect, OutTimeout, OutAborted:
		return true
	}
	return false
}

func msFrom(ns, base int64) *float64 {
	v := math.Round(float64(ns-base)/1e3) / 1e3
	return &v
}

// evaluate judges one probe of one run on one verified block. It retains the
// original receive timestamp of the earliest correct response.
func evaluate(run *ProbeRun, probe string, t *schema.Target, ref *Reference, slotDur int64) *Outcome {
	res := t.Probes[probe]
	if res == nil {
		return &Outcome{Status: OutUnsupported}
	}
	o := &Outcome{
		Attempts:       res.Attempts,
		SkippedPolls:   res.SkippedPolls,
		ProbeStatus:    res.Status,
		OrderViolation: res.OrderViolation,
		ClockStep:      t.ClockStep,
	}
	switch {
	case t.LateArmed:
		o.Status = OutLateArmed
		return o
	case ref.Status == RefOrphaned:
		o.Status, o.Reason = OutOrphaned, ref.Reason
		return o
	case ref.Status != RefVerified:
		o.Status, o.Reason = OutUnverified, ref.Reason
		return o
	}

	base := int64(t.SlotStart)
	if !res.FirstSent.IsZero() {
		o.FirstSentMs = msFrom(int64(res.FirstSent.Wall), base)
	}

	if res.Status == schema.StatusNotApplicable {
		if applicable(probe, ref) {
			o.Status, o.Reason = OutWrongNA, "probe judged the block empty but the reference has data"
		} else {
			o.Status = OutNotApplicable
		}
		return o
	}
	if !applicable(probe, ref) {
		o.Status, o.Reason = OutNotApplicable, "reference block has no data for this probe"
		return o
	}

	expected := ref.Expected[probe]
	var lower *schema.Attempt
	var match *schema.Attempt
	sawData := false
	attempts := run.Attempts[probe][t.BlockNumber]
	for i := range attempts {
		a := &attempts[i]
		if a.Class == schema.ClassResult && a.Digest == expected && expected != "" {
			match = a
			break
		}
		switch {
		case a.Class == schema.ClassNotReady:
			lower = a
		case a.Class == schema.ClassResult:
			sawData = true
			lower = a
		}
	}

	if match == nil {
		switch {
		case res.Status == schema.StatusAborted:
			o.Status, o.Reason = OutAborted, res.Reason
		case sawData:
			o.Status, o.Reason = OutIncorrect, "no response matched the verified reference data"
		default:
			o.Status, o.Reason = OutTimeout, "no data response before the probe stopped"
		}
		if res.MatchDigest != "" {
			o.LocalDisagrees = true
		}
		return o
	}

	o.Status = OutMatched
	o.readyNs = int64(match.Received.Wall)
	wall := strconv.FormatInt(o.readyNs, 10)
	o.ReadyWall = &wall
	o.FreshnessMs = msFrom(o.readyNs, base)
	within := o.readyNs <= base+slotDur
	o.WithinSlot = &within
	if lower != nil {
		o.lowerNs, o.hasLower = int64(lower.Sent.Wall), true
		o.LowerMs = msFrom(o.lowerNs, base)
		o.WeakLowerBound = lower.WeakMatch
	}
	o.LeftCensored = match.Seq == 0 || lower == nil
	if res.MatchDigest != "" && res.MatchDigest != expected {
		o.LocalDisagrees = true
	}
	return o
}

// applicable reports whether the reference block has records for a probe.
// State probes always apply: every block writes the EIP-2935 slot.
func applicable(probe string, ref *Reference) bool {
	switch probe {
	case schema.ProbeLogsNumber, schema.ProbeLogsHash:
		return ref.LogCount > 0
	case schema.ProbeTransactionReceipt, schema.ProbeBlockReceipts:
		return ref.TxCount > 0
	}
	return true
}
