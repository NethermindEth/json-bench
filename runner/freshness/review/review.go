package review

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/jsonrpc-bench/runner/freshness/capture"
	"github.com/jsonrpc-bench/runner/freshness/probe"
	"github.com/jsonrpc-bench/runner/freshness/schema"
)

type Options struct {
	Offline bool
	Refresh bool
	Logger  logrus.FieldLogger
}

// BlockResult is one line of block-results.jsonl: every pair's verified
// outcome for one block identity.
type BlockResult struct {
	BlockNumber       uint64                         `json:"block_number"`
	BlockHash         string                         `json:"block_hash"`
	SlotStart         schema.Nanos                   `json:"slot_start_ns"`
	Slot              *uint64                        `json:"slot,omitempty"`
	MissedSlotsBefore int                            `json:"missed_slots_before"`
	Reference         *Reference                     `json:"reference"`
	Results           map[string]map[string]*Outcome `json:"results"`
	Timeline          map[string]*Timeline           `json:"timeline,omitempty"`
}

type Timeline struct {
	HeaderObservedMs *float64 `json:"header_observed_ms,omitempty"`
	HeaderSource     string   `json:"header_source,omitempty"`
	CL               []CLMark `json:"cl_events,omitempty"`
}

type CLMark struct {
	Topic string  `json:"topic"`
	Ms    float64 `json:"ms"`
}

type ProbeInfo struct {
	PairID      string              `json:"pair_id"`
	HostID      string              `json:"host_id"`
	ClockDomain string              `json:"clock_domain"`
	Labels      map[string]string   `json:"labels,omitempty"`
	RunID       string              `json:"run_id"`
	Dir         string              `json:"dir"`
	ELVersion   string              `json:"el_client_version,omitempty"`
	CLVersion   string              `json:"cl_client_version,omitempty"`
	FirstBlock  uint64              `json:"first_block"`
	LastBlock   uint64              `json:"last_block"`
	Outcome     string              `json:"outcome"`
	Clock       schema.ClockQuality `json:"clock"`
	RTTStart    *schema.RTTStats    `json:"rtt_start,omitempty"`
	RTTEnd      *schema.RTTStats    `json:"rtt_end,omitempty"`
	Dropped     map[string]int64    `json:"dropped_records"`
	Workload    Workload            `json:"workload"`
	Unsupported map[string]string   `json:"unsupported_probes,omitempty"`
}

// Workload is the part of a probe config that shapes the load on the node;
// runs with different workloads are reported, not silently compared as equal.
type Workload struct {
	PollIntervalMs          int      `json:"poll_interval_ms"`
	LookaheadPollIntervalMs int      `json:"lookahead_poll_interval_ms"`
	RequestTimeoutMs        int      `json:"request_timeout_ms"`
	MaxInflightPerProbe     int      `json:"max_inflight_per_probe"`
	LogsStablePolls         int      `json:"logs_stable_polls"`
	Probes                  []string `json:"probes"`
}

type ProbeSummary struct {
	Pairs       map[string]*PairStats `json:"pairs"`
	Comparisons []*Comparison         `json:"comparisons"`
}

type Summary struct {
	SchemaVersion int                      `json:"schema_version"`
	ChainID       uint64                   `json:"chain_id"`
	MarginMs      float64                  `json:"margin_ms"`
	DeadlinesMs   []float64                `json:"deadlines_ms"`
	SlotSeconds   uint64                   `json:"slot_duration_seconds"`
	Probes        []ProbeInfo              `json:"probes"`
	Reference     map[string]any           `json:"reference"`
	Blocks        map[string]int           `json:"blocks"`
	Warnings      []string                 `json:"warnings"`
	Results       map[string]*ProbeSummary `json:"results"`
}

type Result struct {
	Dir     string
	Summary *Summary
	Blocks  []*BlockResult
}

func Run(ctx context.Context, cfg *Config, opts Options) (*Result, error) {
	if err := cfg.Validate(opts.Offline); err != nil {
		return nil, err
	}
	log := opts.Logger
	if log == nil {
		log = logrus.StandardLogger()
	}

	runs, warnings, err := loadRuns(cfg.Probes)
	if err != nil {
		return nil, err
	}
	sum := &Summary{
		SchemaVersion: schema.Version,
		ChainID:       runs[0].Manifest.ChainID,
		MarginMs:      cfg.MarginMs,
		DeadlinesMs:   cfg.DeadlinesMs,
		SlotSeconds:   runs[0].Manifest.SlotDurationSeconds,
		Warnings:      warnings,
		Results:       map[string]*ProbeSummary{},
		Blocks:        map[string]int{},
	}
	for _, r := range runs {
		sum.Probes = append(sum.Probes, probeInfo(r))
	}
	sum.Warnings = append(sum.Warnings, workloadWarnings(sum.Probes)...)
	sum.Warnings = append(sum.Warnings, qualityWarnings(sum.Probes, cfg.MarginMs)...)

	hashes := map[string]uint64{}
	for _, r := range runs {
		for _, t := range r.Targets {
			if t.BlockHash != "" && (cfg.IncludeWarmup || !t.Warmup) {
				hashes[t.BlockHash] = t.BlockNumber
			}
		}
	}
	blocks := sortedBlocks(hashes)

	if err := os.MkdirAll(cfg.OutputDirectory, 0o755); err != nil {
		return nil, err
	}
	refDir := filepath.Join(cfg.OutputDirectory, schema.ReferenceDir)
	if !opts.Offline {
		log.Infof("fetching reference data for %d blocks from %s", len(blocks), probe.RedactURL(cfg.Reference.RPCURL))
		if err := fetchReferences(ctx, cfg, refDir, blocks, opts.Refresh); err != nil {
			return nil, err
		}
	}
	refs, chain := loadReferences(refDir, blocks)
	sum.Reference = map[string]any{"available": chain != nil}
	if chain != nil {
		if chain.ChainID != sum.ChainID {
			return nil, fmt.Errorf("reference chain id %d differs from probes' %d", chain.ChainID, sum.ChainID)
		}
		sum.Reference["client_version"] = chain.ClientVersion
		sum.Reference["rpc_url"] = chain.URL
		sum.Reference["head"] = chain.Head
		if chain.HasFinalized {
			sum.Reference["finalized"] = chain.Finalized
		}
	}

	probeKinds := probeUnion(runs)
	slotDur := int64(sum.SlotSeconds) * 1e9
	var results []*BlockResult
	for _, b := range blocks {
		ref := refs[b.hash]
		sum.Blocks[ref.Status]++
		results = append(results, buildBlock(runs, probeKinds, b, ref, slotDur, cfg.IncludeWarmup))
	}
	for _, br := range results {
		if br.Reference.Status == RefVerified && !br.Reference.Finalized && chain != nil && chain.HasFinalized {
			sum.Blocks["verified_not_finalized"]++
		}
		if br.MissedSlotsBefore > 0 {
			sum.Blocks["after_missed_slots"]++
		}
	}

	for _, p := range probeKinds {
		sum.Results[p] = summarise(cfg, runs, results, p, float64(sum.SlotSeconds)*1000)
	}
	sort.Strings(sum.Warnings)

	if err := writeOutputs(cfg.OutputDirectory, sum, results, cfg); err != nil {
		return nil, err
	}
	return &Result{Dir: cfg.OutputDirectory, Summary: sum, Blocks: results}, nil
}

func loadRuns(dirs []string) ([]*ProbeRun, []string, error) {
	var runs []*ProbeRun
	seen := map[string]string{}
	for _, d := range dirs {
		r, err := LoadProbeRun(d)
		if err != nil {
			return nil, nil, fmt.Errorf("load %s: %w", d, err)
		}
		if prev, dup := seen[r.ID()]; dup {
			return nil, nil, fmt.Errorf("pair id %q appears in both %s and %s", r.ID(), prev, d)
		}
		seen[r.ID()] = d
		runs = append(runs, r)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].ID() < runs[j].ID() })
	var warnings []string
	first := runs[0].Manifest
	for _, r := range runs[1:] {
		m := r.Manifest
		if m.ChainID != first.ChainID {
			return nil, nil, fmt.Errorf("%s is on chain %d, %s on chain %d", r.ID(), m.ChainID, runs[0].ID(), first.ChainID)
		}
		if m.GenesisHash != "" && first.GenesisHash != "" && m.GenesisHash != first.GenesisHash {
			return nil, nil, fmt.Errorf("%s and %s report different genesis blocks", r.ID(), runs[0].ID())
		}
		if m.SlotDurationSeconds != first.SlotDurationSeconds {
			return nil, nil, fmt.Errorf("%s and %s use different slot durations", r.ID(), runs[0].ID())
		}
	}
	for _, r := range runs {
		if r.Manifest.GenesisHash == "" {
			warnings = append(warnings, fmt.Sprintf("%s: genesis hash unknown, genesis identity not confirmed", r.ID()))
		}
		if r.Manifest.Outcome != schema.OutcomeCompleted {
			warnings = append(warnings, fmt.Sprintf("%s: probe run ended with %s %s", r.ID(), r.Manifest.Outcome, r.Manifest.OutcomeReason))
		}
		for name, n := range r.Manifest.Dropped {
			if n > 0 {
				warnings = append(warnings, fmt.Sprintf("%s: %d %s records were dropped during capture", r.ID(), n, name))
			}
		}
	}
	return runs, warnings, nil
}

func probeInfo(r *ProbeRun) ProbeInfo {
	m := r.Manifest
	info := ProbeInfo{
		PairID: m.PairID, HostID: m.HostID, ClockDomain: m.ClockDomain, Labels: m.Labels, RunID: m.RunID, Dir: r.Dir,
		ELVersion: m.ELClientVersion, CLVersion: m.CLClientVersion, FirstBlock: m.FirstBlock, LastBlock: m.LastBlock,
		Outcome: m.Outcome, Clock: m.Clock, RTTStart: m.RTTStart, RTTEnd: m.RTTEnd, Dropped: m.Dropped,
	}
	var cfg probe.Config
	if b, err := json.Marshal(m.Config); err == nil && json.Unmarshal(b, &cfg) == nil {
		info.Workload = Workload{
			PollIntervalMs: cfg.PollIntervalMs, LookaheadPollIntervalMs: cfg.LookaheadPollIntervalMs,
			RequestTimeoutMs: cfg.RequestTimeoutMs, MaxInflightPerProbe: cfg.MaxInflightPerProbe,
			LogsStablePolls: cfg.LogsStablePolls, Probes: cfg.EnabledProbes(),
		}
	}
	for p, c := range r.Capabilities.Probes {
		if !c.Supported {
			if info.Unsupported == nil {
				info.Unsupported = map[string]string{}
			}
			info.Unsupported[p] = c.Reason
		}
	}
	return info
}

func workloadWarnings(infos []ProbeInfo) []string {
	var out []string
	base, _ := json.Marshal(infos[0].Workload)
	for _, i := range infos[1:] {
		if w, _ := json.Marshal(i.Workload); string(w) != string(base) {
			out = append(out, fmt.Sprintf("%s runs a different workload than %s (%s vs %s); comparisons mix workloads", i.PairID, infos[0].PairID, w, base))
		}
	}
	return out
}

func qualityWarnings(infos []ProbeInfo, margin float64) []string {
	var out []string
	domains := map[string]bool{}
	for _, i := range infos {
		domains[i.ClockDomain] = true
		if strings.Contains(i.Clock.Source, "fallback") || i.Clock.Source == "unavailable" {
			out = append(out, fmt.Sprintf("%s: clock quality not measured (%s); cross-host margins rely on the configured budget", i.PairID, i.Clock.Source))
		}
		if i.Clock.Steps > 0 {
			out = append(out, fmt.Sprintf("%s: %d wall-clock steps detected; affected blocks are excluded from cross-host comparisons", i.PairID, i.Clock.Steps))
		}
	}
	for a := 0; a < len(infos); a++ {
		for b := a + 1; b < len(infos); b++ {
			ra, rb := infos[a].RTTStart, infos[b].RTTStart
			if ra == nil || rb == nil {
				continue
			}
			diff := math.Abs(float64(ra.P50Micro-rb.P50Micro)) / 1000
			if diff >= margin {
				out = append(out, fmt.Sprintf("%s and %s differ by %.1f ms in idle RTT to their nodes; that difference is part of their freshness", infos[a].PairID, infos[b].PairID, diff))
			}
		}
	}
	return out
}

func probeUnion(runs []*ProbeRun) []string {
	set := map[string]bool{}
	for _, r := range runs {
		for p := range r.Capabilities.Probes {
			set[p] = true
		}
	}
	var out []string
	for _, p := range schema.AllProbes {
		if set[p] {
			out = append(out, p)
		}
	}
	return out
}

func buildBlock(runs []*ProbeRun, kinds []string, b block, ref *Reference, slotDur int64, includeWarmup bool) *BlockResult {
	br := &BlockResult{BlockNumber: b.number, BlockHash: b.hash, Reference: ref,
		Results: map[string]map[string]*Outcome{}, Timeline: map[string]*Timeline{}}
	for _, r := range runs {
		per := map[string]*Outcome{}
		br.Results[r.ID()] = per
		t := r.Targets[b.number]
		fill := func(status string) {
			for _, p := range kinds {
				per[p] = &Outcome{Status: status}
			}
		}
		switch {
		case !r.InRange(b.number):
			fill(OutOutOfRange)
			continue
		case t == nil:
			fill(OutNotObserved)
			continue
		case t.BlockHash != b.hash:
			fill(OutDifferentBlock)
			continue
		case t.Warmup && !includeWarmup:
			fill(OutWarmup)
			continue
		}
		if br.SlotStart == 0 {
			br.SlotStart, br.Slot = t.SlotStart, t.Slot
		}
		if t.MissedSlots > br.MissedSlotsBefore {
			br.MissedSlotsBefore = t.MissedSlots
		}
		for _, p := range kinds {
			if c := r.Capabilities.Probes[p]; c == nil || !c.Supported {
				per[p] = &Outcome{Status: OutUnsupported}
				continue
			}
			per[p] = evaluate(r, p, t, ref, slotDur)
		}
		tl := &Timeline{HeaderSource: t.HeaderSource}
		if t.HeaderObserved != nil {
			tl.HeaderObservedMs = msFrom(int64(t.HeaderObserved.Wall), int64(t.SlotStart))
		}
		if t.Slot != nil {
			for _, e := range r.CLEvents[*t.Slot] {
				tl.CL = append(tl.CL, CLMark{Topic: strings.TrimPrefix(e.Source, "beacon_sse:"), Ms: *msFrom(int64(e.At.Wall), int64(t.SlotStart))})
			}
		}
		br.Timeline[r.ID()] = tl
	}
	return br
}

type group struct {
	name    string
	members []*ProbeRun
}

func groups(cfg *Config, runs []*ProbeRun) []group {
	out := []group{{name: "all", members: runs}}
	for _, key := range cfg.HoldConstant {
		byValue := map[string][]*ProbeRun{}
		for _, r := range runs {
			if v, ok := r.Manifest.Labels[key]; ok {
				byValue[v] = append(byValue[v], r)
			}
		}
		values := make([]string, 0, len(byValue))
		for v := range byValue {
			values = append(values, v)
		}
		sort.Strings(values)
		for _, v := range values {
			if len(byValue[v]) >= 2 {
				out = append(out, group{name: key + "=" + v, members: byValue[v]})
			}
		}
	}
	return out
}

func summarise(cfg *Config, runs []*ProbeRun, results []*BlockResult, kind string, slotMs float64) *ProbeSummary {
	ps := &ProbeSummary{Pairs: map[string]*PairStats{}}
	for _, r := range runs {
		var outs []*Outcome
		for _, br := range results {
			o := br.Results[r.ID()][kind]
			if o == nil {
				continue
			}
			switch o.Status {
			case OutOutOfRange, OutWarmup:
				continue
			case OutOrphaned, OutUnverified:
			default:
				if br.Reference.Status != RefVerified {
					continue
				}
			}
			outs = append(outs, o)
		}
		ps.Pairs[r.ID()] = pairStats(outs, cfg.DeadlinesMs, slotMs)
	}
	for _, g := range groups(cfg, runs) {
		for i := 0; i < len(g.members); i++ {
			for j := i + 1; j < len(g.members); j++ {
				ps.Comparisons = append(ps.Comparisons, comparePair(cfg, g.name, g.members[i], g.members[j], results, kind))
			}
		}
	}
	return ps
}

func comparePair(cfg *Config, groupName string, a, b *ProbeRun, results []*BlockResult, kind string) *Comparison {
	c := &Comparison{Group: groupName, A: a.ID(), B: b.ID(), Excluded: map[string]int{},
		CrossHost: a.Manifest.ClockDomain != b.Manifest.ClockDomain}
	margin := cfg.MarginMs
	if c.CrossHost {
		margin = math.Max(margin, a.ClockErrorMs()+b.ClockErrorMs())
	}
	c.MarginMs = round3(margin)
	var deltas, aw, bw []float64
	for _, br := range results {
		if br.Reference.Status != RefVerified {
			continue
		}
		oa, ob := br.Results[a.ID()][kind], br.Results[b.ID()][kind]
		if oa != nil && ob != nil && (oa.Status == OutOutOfRange || ob.Status == OutOutOfRange || oa.Status == OutWarmup || ob.Status == OutWarmup) {
			continue
		}
		c.add(side{a, oa}, side{b, ob}, margin, &deltas, &aw, &bw)
	}
	c.Delta, c.AWinMargin, c.BWinMargin = newDist(deltas), newDist(aw), newDist(bw)
	return c
}

func writeOutputs(dir string, sum *Summary, results []*BlockResult, cfg *Config) error {
	w, err := capture.NewJSONL(filepath.Join(dir, schema.BlockResultsFile), len(results)+1)
	if err != nil {
		return err
	}
	for _, br := range results {
		w.Write(br)
	}
	if err := w.Close(); err != nil {
		return err
	}
	if err := capture.WriteJSON(filepath.Join(dir, schema.SummaryFile), sum); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, schema.ReportFile), []byte(renderReport(sum, results, cfg)), 0o644)
}
