package review

import "math"

// CL milestones split freshness into "the pair's CL reached this milestone"
// and "its EL made the data readable after it". Beacon API events are
// post-validation milestones seen by the probe, not network arrival, and are
// only present when the probe had a beacon URL.
var clTopics = []string{"block_gossip", "head", "block"}

// CLSplit is one pair's freshness split at one milestone. Both parts are
// measured on the probe host; ReadyAfter is a same-clock difference, so it is
// free of cross-host clock error.
type CLSplit struct {
	Blocks     int   `json:"blocks_with_event"`
	Missing    int   `json:"measured_without_event"`
	Milestone  *Dist `json:"milestone_from_slot_start,omitempty"`
	ReadyAfter *Dist `json:"ready_after_milestone,omitempty"`
}

// PairedCLSplit compares two pairs block by block at one milestone, over
// blocks both matched and both saw the event.
type PairedCLSplit struct {
	Compared        int   `json:"compared"`
	MilestoneDelta  *Dist `json:"milestone_delta_a_minus_b,omitempty"`
	ReadyAfterA     int   `json:"ready_after_a_faster"`
	ReadyAfterB     int   `json:"ready_after_b_faster"`
	ReadyAfterTies  int   `json:"ready_after_ties"`
	ReadyAfterDelta *Dist `json:"ready_after_delta_a_minus_b,omitempty"`
}

func firstMark(tl *Timeline, topic string) (float64, bool) {
	if tl == nil {
		return 0, false
	}
	for _, m := range tl.CL {
		if m.Topic == topic {
			return m.Ms, true
		}
	}
	return 0, false
}

// pairCLSplit summarises measured blocks (outs[i] with timeline tls[i]).
func pairCLSplit(outs []*Outcome, tls []*Timeline) map[string]*CLSplit {
	out := map[string]*CLSplit{}
	for _, topic := range clTopics {
		var milestone, after []float64
		missing := 0
		for i, o := range outs {
			if !o.Comparable() {
				continue
			}
			m, ok := firstMark(tls[i], topic)
			if !ok {
				missing++
				continue
			}
			milestone = append(milestone, m)
			if o.Status == OutMatched {
				after = append(after, *o.FreshnessMs-m)
			}
		}
		if len(milestone) > 0 {
			out[topic] = &CLSplit{Blocks: len(milestone), Missing: missing, Milestone: newDist(milestone), ReadyAfter: newDist(after)}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func pairedCLSplit(results []*BlockResult, a, b, kind string, margin float64, topics []string) map[string]*PairedCLSplit {
	out := map[string]*PairedCLSplit{}
	for _, topic := range topics {
		p := &PairedCLSplit{}
		var mDelta, rDelta []float64
		for _, br := range results {
			if br.Reference.Status != RefVerified {
				continue
			}
			oa, ob := br.Results[a][kind], br.Results[b][kind]
			if oa == nil || ob == nil || oa.Status != OutMatched || ob.Status != OutMatched {
				continue
			}
			ma, okA := firstMark(br.Timeline[a], topic)
			mb, okB := firstMark(br.Timeline[b], topic)
			if !okA || !okB {
				continue
			}
			p.Compared++
			mDelta = append(mDelta, ma-mb)
			d := (*oa.FreshnessMs - ma) - (*ob.FreshnessMs - mb)
			rDelta = append(rDelta, d)
			switch {
			case math.Abs(d) < margin:
				p.ReadyAfterTies++
			case d < 0:
				p.ReadyAfterA++
			default:
				p.ReadyAfterB++
			}
		}
		if p.Compared > 0 {
			p.MilestoneDelta, p.ReadyAfterDelta = newDist(mDelta), newDist(rDelta)
			out[topic] = p
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
