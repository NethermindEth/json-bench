package review

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/jsonrpc-bench/runner/freshness/schema"
)

func renderReport(sum *Summary, results []*BlockResult, cfg *Config) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	w("# RPC freshness review\n\n")
	w("Freshness is the time from the block's slot start to the first response later verified correct against the reference. ")
	w("It includes proposer timing, block delivery, execution and RPC exposure; it is not pure propagation latency.\n\n")
	w("- Chain ID: %d, slot duration %d s\n", sum.ChainID, sum.SlotSeconds)
	w("- Reference: %s\n", describeReference(sum.Reference))
	w("- Meaningful margin: %g ms (raised to the combined clock error for cross-host pairs)\n", sum.MarginMs)
	w("- Blocks: %s\n", describeCounts(sum.Blocks))
	if n := sum.Blocks["verified_not_finalized"]; n > 0 {
		w("- %s verified before finalization: correctness holds, but canonicality is as of the review. Re-run with `--refresh-reference` about two epochs later to confirm.\n", plural(n, "block was", "blocks were"))
	}
	if n := sum.Blocks["epoch_boundary"]; n > 0 {
		w("- %s the first slot of an epoch (marked `E`): CL epoch processing can delay import independently of block contents.\n", plural(n, "block is", "blocks are"))
	}
	if n := sum.Blocks["optimistic_import"]; n > 0 {
		w("- %s imported optimistically by at least one pair's CL (marked `O` in the per-block table): its EL had not validated the block yet, typically because it was busy.\n", plural(n, "block was", "blocks were"))
	}
	w("\n")

	kinds := make([]string, 0, len(sum.Results))
	for _, k := range schema.AllProbes {
		if _, ok := sum.Results[k]; ok {
			kinds = append(kinds, k)
		}
	}
	renderSummary(&b, sum, kinds)

	if len(sum.Warnings) > 0 {
		w("## Warnings\n\n")
		for _, x := range sum.Warnings {
			w("- %s\n", x)
		}
		w("\n")
	}

	w("## Probes\n\n")
	w("| Pair | Host | Labels | EL | CL | Blocks | Outcome | Clock error (ms) | Idle RTT p50 (ms) | Dropped |\n")
	w("|---|---|---|---|---|---|---|---|---|---|\n")
	for _, p := range sum.Probes {
		rtt := "-"
		if p.RTTStart != nil && p.RTTStart.Samples > 0 {
			rtt = fmt.Sprintf("%.2f", float64(p.RTTStart.P50Micro)/1000)
		}
		dropped := int64(0)
		for _, n := range p.Dropped {
			dropped += n
		}
		w("| %s | %s | %s | %s | %s | %d-%d | %s | %.2f (%s) | %s | %d |\n", p.PairID, p.HostID, labels(p.Labels),
			orDash(p.ELVersion), orDash(p.CLVersion), p.FirstBlock, p.LastBlock, p.Outcome, math.Max(p.Clock.ErrorMs, p.Clock.MaxErrorMs), p.Clock.Source, rtt, dropped)
	}
	w("\n")

	pairs := make([]string, 0, len(sum.Probes))
	for _, p := range sum.Probes {
		pairs = append(pairs, p.PairID)
	}

	for _, k := range kinds {
		ps := sum.Results[k]
		w("## Details: %s\n\n", k)
		w("Every column is explained in `%s` (section \"Reading the report\").\n\n", docsLink)
		if k == schema.ProbeStateLatest {
			w("`latest` answers show *at-least-N* freshness: the expected value stays readable in later blocks.\n\n")
		}
		w("### Freshness and coverage\n\n")
		w("| Pair | Measured | Matched | p50 | p95 | p99 | Min | Max | Left-censored | Outside slot | Coverage |\n")
		w("|---|---|---|---|---|---|---|---|---|---|---|\n")
		for _, id := range pairs {
			st := ps.Pairs[id]
			d := st.Freshness
			if d == nil {
				d = &Dist{}
			}
			w("| %s | %d | %d | %s | %s | %s | %s | %s | %d | %d | %s |\n", id, st.Denominator, st.Status[OutMatched],
				ms(d.P50, d.N), ms(d.P95, d.N), ms(d.P99, d.N), ms(d.Min, d.N), ms(d.Max, d.N), st.LeftCensored, st.OutsideSlot, describeCounts(st.Status))
		}
		w("\n### Availability by deadline (share of measured blocks)\n\n")
		keys := availabilityKeys(cfg.DeadlinesMs)
		w("| Pair | %s |\n|---|%s\n", strings.Join(keys, " | "), strings.Repeat("---|", len(keys)))
		for _, id := range pairs {
			row := make([]string, len(keys))
			for i, key := range keys {
				row[i] = fmt.Sprintf("%.1f%%", ps.Pairs[id].Availability[key]*100)
			}
			w("| %s | %s |\n", id, strings.Join(row, " | "))
		}
		w("\n### Pairwise\n\n")
		w("Each row counts blocks. *First correct*: whose correct answer arrived first (tie if closer than the margin). ")
		w("*Certain*: the same, but only when the two availability windows (last \"not ready\" to first correct answer, widened by clock error) do not overlap. ")
		w("*Only one correct*: only that side ever returned correct data. Δ is A minus B: negative means A was earlier.\n\n")
		w("| Group | A | B | Compared | First correct A / B / tie | Certain A / B / unclear | Only one correct A / B | Both failed | Median Δ A−B (ms) | Median lead when A / B first | Margin (ms) | Excluded |\n")
		w("|---|---|---|---|---|---|---|---|---|---|---|---|\n")
		for _, c := range ps.Comparisons {
			delta, am, bm := "-", "-", "-"
			if c.Delta != nil {
				delta = fmt.Sprintf("%+.1f", c.Delta.P50)
			}
			if c.AWinMargin != nil {
				am = fmt.Sprintf("%.1f", c.AWinMargin.P50)
			}
			if c.BWinMargin != nil {
				bm = fmt.Sprintf("%.1f", c.BWinMargin.P50)
			}
			w("| %s | %s | %s | %d | %d/%d/%d | %d/%d/%d | %d/%d | %d | %s | %s / %s | %g | %s |\n", c.Group, c.A, c.B, c.Compared,
				c.ObservedAWins, c.ObservedBWins, c.ObservedTies, c.InferredAWins, c.InferredBWins, c.Unresolved,
				c.CoverageAWins, c.CoverageBWins, c.BothFailed, delta, am, bm, c.MarginMs, describeCounts(c.Excluded))
		}
		w("\nCompare pairs by the per-block median Δ and win counts, not by the difference of their p50s: the medians of two distributions can order differently from per-block results.\n\n")
		renderContext(&b, k, ps, pairs)
		renderCLSplit(&b, ps, pairs)
	}

	if len(kinds) > 0 {
		primary := kinds[0]
		w("## Per-block (%s)\n\n", primary)
		w("Milliseconds from slot start; non-matches show their status.\n\n")
		w("| Block | Hash | Missed slots | Epoch | Reference | %s |\n|---|---|---|---|---|%s\n", strings.Join(pairs, " | "), strings.Repeat("---|", len(pairs)))
		for _, br := range results {
			cells := make([]string, len(pairs))
			for i, id := range pairs {
				cells[i] = cell(br.Results[id][primary], br.Timeline[id])
			}
			w("| %d | %s | %d | %s | %s | %s |\n", br.BlockNumber, short(br.BlockHash), br.MissedSlotsBefore, epochMark(br), br.Reference.Status, strings.Join(cells, " | "))
		}
		w("\n")
		renderTimelines(&b, results, pairs, kinds, cfg.TimelineBlocks)
	}
	return b.String()
}

// renderTimelines drills into the blocks where pairs disagreed most on the
// primary probe.
func renderTimelines(b *strings.Builder, results []*BlockResult, pairs, kinds []string, limit int) {
	type spread struct {
		br *BlockResult
		d  float64
	}
	var ranked []spread
	for _, br := range results {
		lo, hi, n := math.Inf(1), math.Inf(-1), 0
		for _, id := range pairs {
			if o := br.Results[id][kinds[0]]; o != nil && o.FreshnessMs != nil {
				lo, hi, n = math.Min(lo, *o.FreshnessMs), math.Max(hi, *o.FreshnessMs), n+1
			}
		}
		if n >= 2 {
			ranked = append(ranked, spread{br, hi - lo})
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].d > ranked[j].d })
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	if len(ranked) == 0 {
		return
	}
	fmt.Fprintf(b, "## Timelines (largest spread on %s)\n\n", kinds[0])
	for _, s := range ranked {
		br := s.br
		epoch := ""
		if br.EpochBoundary {
			epoch = ", epoch boundary"
		}
		fmt.Fprintf(b, "### Block %d %s (spread %.1f ms%s)\n\n", br.BlockNumber, short(br.BlockHash), s.d, epoch)
		fmt.Fprintf(b, "| Pair | Header observed | CL events | %s |\n|---|---|---|%s\n", strings.Join(kinds, " | "), strings.Repeat("---|", len(kinds)))
		for _, id := range pairs {
			tl := br.Timeline[id]
			header, cl := "-", "-"
			if tl != nil {
				if tl.HeaderObservedMs != nil {
					header = fmt.Sprintf("%+.1f (%s)", *tl.HeaderObservedMs, tl.HeaderSource)
				}
				if len(tl.CL) > 0 {
					parts := make([]string, len(tl.CL))
					for i, m := range tl.CL {
						parts[i] = fmt.Sprintf("%s %+.1f", m.Topic, m.Ms)
					}
					cl = strings.Join(parts, ", ")
				}
			}
			cells := make([]string, len(kinds))
			for i, k := range kinds {
				cells[i] = timelineCell(br.Results[id][k])
			}
			fmt.Fprintf(b, "| %s | %s | %s | %s |\n", id, header, cl, strings.Join(cells, " | "))
		}
		b.WriteString("\n")
	}
}

func timelineCell(o *Outcome) string {
	if o == nil {
		return "-"
	}
	if o.Status != OutMatched {
		return o.Status
	}
	parts := []string{}
	if o.FirstSentMs != nil {
		parts = append(parts, fmt.Sprintf("first %+.1f", *o.FirstSentMs))
	}
	if o.LowerMs != nil && !o.LeftCensored {
		parts = append(parts, fmt.Sprintf("not-ready %+.1f", *o.LowerMs))
	}
	parts = append(parts, fmt.Sprintf("**ready %+.1f**", *o.FreshnessMs))
	if o.LeftCensored {
		parts = append(parts, "left-censored")
	}
	return strings.Join(parts, " → ")
}

func cell(o *Outcome, tl *Timeline) string {
	if o == nil {
		return "-"
	}
	s := o.Status
	if o.Status == OutMatched {
		s = fmt.Sprintf("%.1f", *o.FreshnessMs)
		if o.LeftCensored {
			s += "*"
		}
	}
	if tl != nil && tl.Optimistic {
		s += " O"
	}
	return s
}

// renderContext separates conditions that move freshness for reasons outside
// the EL, plus how far this probe trails the state canary.
func renderContext(b *strings.Builder, kind string, ps *ProbeSummary, pairs []string) {
	has := false
	for _, id := range pairs {
		st := ps.Pairs[id]
		if st.FreshnessEpoch != nil || st.OptimisticImports > 0 || st.LagVsState != nil {
			has = true
		}
	}
	if !has {
		return
	}
	w := func(format string, args ...any) { fmt.Fprintf(b, format, args...) }
	w("### Context\n\n")
	w("Epoch-boundary blocks (first slot of an epoch) include CL epoch processing. Optimistic imports: blocks the CL imported before its EL validated them. ")
	if kind != schema.ProbeStateNumber {
		w("Lag vs state: this probe's freshness minus `state_number`'s on the same block, e.g. asynchronous log indexing.")
	}
	w("\n\n| Pair | Optimistic imports | Epoch-boundary p50 / p95 (n) | Other blocks p50 / p95 (n) | Lag vs state p50 / p95 |\n|---|---|---|---|---|\n")
	for _, id := range pairs {
		st := ps.Pairs[id]
		w("| %s | %d | %s | %s | %s |\n", id, st.OptimisticImports, distN(st.FreshnessEpoch), distN(st.FreshnessNonEpoch), distPair(st.LagVsState))
	}
	w("\n")
}

func distN(d *Dist) string {
	if d == nil {
		return "-"
	}
	return fmt.Sprintf("%.1f / %.1f (%d)", d.P50, d.P95, d.N)
}

func distPair(d *Dist) string {
	if d == nil {
		return "-"
	}
	return fmt.Sprintf("%.1f / %.1f", d.P50, d.P95)
}

func availabilityKeys(deadlines []float64) []string {
	keys := make([]string, 0, len(deadlines)+1)
	for _, d := range deadlines {
		keys = append(keys, fmt.Sprintf("%gms", d))
	}
	return append(keys, "slot")
}

func describeCounts(m map[string]int) string {
	if len(m) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, m[k]))
	}
	return strings.Join(parts, ", ")
}

func describeReference(r map[string]any) string {
	if r["available"] != true {
		return "no reference data (every block unverified)"
	}
	var parts []string
	for _, k := range []string{"rpc_url", "client_version", "head", "finalized"} {
		if v, ok := r[k]; ok && fmt.Sprint(v) != "" {
			parts = append(parts, fmt.Sprintf("%s %v", k, v))
		}
	}
	return strings.Join(parts, ", ")
}

func labels(m map[string]string) string {
	if len(m) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + m[k]
	}
	return strings.Join(parts, " ")
}

func ms(v float64, n int) string {
	if n == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f", v)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func short(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

func epochMark(br *BlockResult) string {
	if br.EpochBoundary {
		return "E"
	}
	return ""
}

// renderCLSplit shows where freshness is spent relative to each beacon
// milestone. Omitted when no probe had a beacon URL.
func renderCLSplit(b *strings.Builder, ps *ProbeSummary, pairs []string) {
	has := false
	for _, id := range pairs {
		if len(ps.Pairs[id].CL) > 0 {
			has = true
		}
	}
	if !has {
		return
	}
	w := func(format string, args ...any) { fmt.Fprintf(b, format, args...) }
	w("### Split at CL milestones\n\n")
	w("Milestone: when the pair's beacon node emitted the event, from slot start (a post-validation milestone seen by the probe, not network arrival). ")
	w("Ready after: freshness minus that milestone, i.e. the time spent after it. Which work each milestone includes is client-specific (Lighthouse emits `head` before `block`). ")
	w("Ready after is measured on one host, so it carries no cross-host clock error.\n\n")
	w("| Pair | Milestone | Blocks | Missing | Milestone p50 | Milestone p95 | Ready after p50 | Ready after p95 |\n|---|---|---|---|---|---|---|---|\n")
	for _, id := range pairs {
		for _, topic := range clTopics {
			sp := ps.Pairs[id].CL[topic]
			if sp == nil {
				continue
			}
			w("| %s | %s | %d | %d | %s | %s | %s | %s |\n", id, topic, sp.Blocks, sp.Missing, distCell(sp.Milestone, false), distCell(sp.Milestone, true),
				distCell(sp.ReadyAfter, false), distCell(sp.ReadyAfter, true))
		}
	}
	w("\nMissing: measured blocks where the pair's CL never emitted that event (e.g. no `head` after an optimistic import).\n\n")
	w("Paired rows, per block where both answered correctly and both CLs emitted the event. *Milestone Δ*: A's event time minus B's (negative: A's CL got there first). ")
	w("*Faster after milestone*: whose data became readable in less time counted from its own event (tie if closer than the margin). ")
	w("Pairs with different CL clients are compared at `block_gossip` only, because each CL emits `head` and `block` at a different point in its import.\n")
	w("\n| Group | A | B | Milestone | Compared | Median milestone Δ A−B (ms) | Faster after milestone A / B / tie | Median Δ after milestone A−B (ms) |\n|---|---|---|---|---|---|---|---|\n")
	for _, c := range ps.Comparisons {
		for _, topic := range clTopics {
			p := c.CL[topic]
			if p == nil {
				continue
			}
			w("| %s | %s | %s | %s | %d | %s | %d/%d/%d | %s |\n", c.Group, c.A, c.B, topic, p.Compared,
				signedCell(p.MilestoneDelta), p.ReadyAfterA, p.ReadyAfterB, p.ReadyAfterTies, signedCell(p.ReadyAfterDelta))
		}
	}
	w("\n")
}

func distCell(d *Dist, p95 bool) string {
	if d == nil {
		return "-"
	}
	if p95 {
		return fmt.Sprintf("%.1f", d.P95)
	}
	return fmt.Sprintf("%.1f", d.P50)
}

func signedCell(d *Dist) string {
	if d == nil {
		return "-"
	}
	return fmt.Sprintf("%+.1f", d.P50)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
