package review

import (
	"fmt"
	"math"
	"strings"

	"github.com/jsonrpc-bench/runner/freshness/schema"
)

const docsLink = "runner/freshness/README.md"

// sameClient reports whether two pairs run the same client for one side
// (`el` or `cl`): by label when both have it, else by the client name in the
// reported version string. Unknown counts as different.
func sameClient(la, lb map[string]string, key, va, vb string) bool {
	x, okA := la[key]
	y, okB := lb[key]
	if okA && okB {
		return strings.EqualFold(x, y)
	}
	na, nb := clientName(va), clientName(vb)
	return na != "" && na == nb
}

func clientOf(p ProbeInfo, key, version string) string {
	if v, ok := p.Labels[key]; ok {
		return v
	}
	return clientName(version)
}

func probeLabel(kind string) string {
	switch kind {
	case schema.ProbeStateNumber:
		return "state"
	case schema.ProbeLogsNumber:
		return "logs"
	}
	return "`" + kind + "`"
}

// renderSummary answers the report's question in prose before any table:
// how long each pair takes, where that time goes, and who wins head to head.
func renderSummary(b *strings.Builder, sum *Summary, kinds []string) {
	if len(kinds) == 0 {
		return
	}
	w := func(format string, args ...any) { fmt.Fprintf(b, format, args...) }
	infos := map[string]ProbeInfo{}
	for _, p := range sum.Probes {
		infos[p.PairID] = p
	}
	primary := kinds[0]
	ps := sum.Results[primary]

	w("## Summary\n\n")
	w("Freshness = time from the block's slot start until the pair first returned correct data for it (lower is better). ")
	w("Column definitions and caveats: `%s`.\n\n", docsLink)

	w("### Where the time goes (%s, median ms from slot start)\n\n", probeLabel(primary))
	w("`▓` until the pair's CL had the block (`block_gossip`), `░` after that until the data was readable. Medians of parts do not add up exactly to the total.\n\n")
	w("| Pair | CL had the block | Then readable after | Readable at | |\n|---|---|---|---|---|\n")
	maxTotal := 0.0
	for _, p := range sum.Probes {
		if d := ps.Pairs[p.PairID].Freshness; d != nil {
			maxTotal = math.Max(maxTotal, d.P50)
		}
	}
	for _, p := range sum.Probes {
		st := ps.Pairs[p.PairID]
		if st.Freshness == nil {
			w("| %s | - | - | no correct answer | |\n", p.PairID)
			continue
		}
		total := st.Freshness.P50
		g := st.CL["block_gossip"]
		if g == nil || g.Milestone == nil || g.ReadyAfter == nil {
			w("| %s | - | - | %.0f | %s |\n", p.PairID, total, bar(0, total, maxTotal))
			continue
		}
		w("| %s | %.0f | +%.0f | %.0f | %s |\n", p.PairID, g.Milestone.P50, g.ReadyAfter.P50, total, bar(g.Milestone.P50, total, maxTotal))
	}
	w("\n")

	w("### Head to head\n\n")
	for _, c := range ps.Comparisons {
		if c.Group != "all" {
			continue
		}
		w("- %s\n", headToHead(c, infos[c.A], infos[c.B], primary, sum.Results))
	}
	w("\n")
}

const barWidth = 30

func bar(split, total, maxTotal float64) string {
	if maxTotal <= 0 || total <= 0 {
		return ""
	}
	n := int(math.Round(total / maxTotal * barWidth))
	s := int(math.Round(math.Min(split, total) / maxTotal * barWidth))
	return "`" + strings.Repeat("▓", s) + strings.Repeat("░", n-s) + "`"
}

func headToHead(c *Comparison, a, b ProbeInfo, primary string, results map[string]*ProbeSummary) string {
	var s strings.Builder
	sameEL := sameClient(a.Labels, b.Labels, "el", a.ELVersion, b.ELVersion)
	sameCLs := sameClient(a.Labels, b.Labels, "cl", a.CLVersion, b.CLVersion)
	var parts []string
	if sameEL {
		parts = append(parts, "same EL: "+clientOf(a, "el", a.ELVersion))
	}
	if sameCLs {
		parts = append(parts, "same CL: "+clientOf(a, "cl", a.CLVersion))
	}
	shared := ""
	if len(parts) > 0 {
		shared = " (" + strings.Join(parts, ", ") + ")"
	}
	fmt.Fprintf(&s, "**%s vs %s**%s. ", c.A, c.B, shared)
	s.WriteString(winSentence(c, probeLabel(primary)))

	if g := c.CL["block_gossip"]; g != nil && g.MilestoneDelta != nil && g.ReadyAfterDelta != nil {
		// Deltas are A−B; restate them from A's point of view in words.
		m, r := g.MilestoneDelta.P50, g.ReadyAfterDelta.P50
		fmt.Fprintf(&s, " %s's CL had the block %s; after that its data was readable %s.", c.A, relative(m, "earlier", "later"), relative(r, "sooner", "later"))
		switch {
		case sameEL && !sameCLs:
			s.WriteString(" With the same EL, the difference after `block_gossip` is on the CL side: validating the block, handing the payload to the EL, and fork choice. This report cannot separate those three.")
		case sameCLs && !sameEL:
			s.WriteString(" With the same CL, the difference after `block_gossip` is mostly the EL (execution and serving), plus host differences.")
		case !sameEL && !sameCLs:
			s.WriteString(" Both clients differ, so the difference cannot be attributed to one of them.")
		}
	}
	for _, k := range []string{schema.ProbeLogsNumber} {
		if k == primary || results[k] == nil {
			continue
		}
		for _, lc := range results[k].Comparisons {
			if lc.Group == "all" && lc.A == c.A && lc.B == c.B {
				s.WriteString(" For " + probeLabel(k) + ": " + lowerFirst(winSentence(lc, probeLabel(k))))
			}
		}
	}
	return s.String()
}

func winSentence(c *Comparison, what string) string {
	extra := ""
	if n := c.CoverageAWins + c.CoverageBWins + c.BothFailed; n > 0 {
		extra = fmt.Sprintf(" On %s at least one side never returned correct data.", plural(n, "more block", "more blocks"))
	}
	matched := c.ObservedAWins + c.ObservedBWins + c.ObservedTies
	if matched == 0 {
		return "No block where both returned correct " + what + "." + extra
	}
	winner, loser, wins, losses := c.A, c.B, c.ObservedAWins, c.ObservedBWins
	if c.ObservedBWins > c.ObservedAWins {
		winner, loser, wins, losses = c.B, c.A, c.ObservedBWins, c.ObservedAWins
	}
	if wins == losses {
		return fmt.Sprintf("Neither was consistently first with %s: %d wins each, %s within %g ms.%s", what, wins, plural(c.ObservedTies, "tie", "ties"), c.MarginMs, extra)
	}
	typical := ""
	if c.Delta != nil {
		typical = fmt.Sprintf(", median difference %.0f ms", math.Abs(c.Delta.P50))
	}
	return fmt.Sprintf("%s had %s readable first on %d of %d blocks (%s on %d, %s)%s.%s",
		winner, what, wins, matched, loser, losses, plural(c.ObservedTies, "tie", "ties"), typical, extra)
}

// relative turns a signed A−B median into words from A's side.
func relative(delta float64, ahead, behind string) string {
	switch {
	case math.Abs(delta) < 1:
		return "at about the same time"
	case delta < 0:
		return fmt.Sprintf("%.0f ms %s", -delta, ahead)
	default:
		return fmt.Sprintf("%.0f ms %s", delta, behind)
	}
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}
