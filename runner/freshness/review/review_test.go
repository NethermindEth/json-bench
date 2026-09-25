package review

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/freshness/mocknode"
	"github.com/jsonrpc-bench/runner/freshness/probe"
	"github.com/jsonrpc-bench/runner/freshness/schema"
)

type probeHandle struct {
	r   *probe.Runner
	err chan error
}

func quietLogger() *logrus.Logger {
	l := logrus.New()
	l.SetLevel(logrus.WarnLevel)
	return l
}

func startProbe(t *testing.T, node *mocknode.Node, id, host string, blocks int, probes map[string]bool, out string, mods ...func(*probe.Config)) *probeHandle {
	t.Helper()
	cfg := &probe.Config{
		Pair:                    probe.PairConfig{ID: id, HostID: host, Labels: map[string]string{"el": id, "cl": "mockcl"}, EL: probe.ELConfig{URL: node.URL()}},
		Chain:                   probe.ChainConfig{SlotDurationSeconds: 1},
		BlockCount:              blocks,
		MaxRunDurationSeconds:   20,
		PollIntervalMs:          2,
		LookaheadPollIntervalMs: 10,
		HeadWatchIntervalMs:     50,
		RequestTimeoutMs:        500,
		Probes:                  probes,
		Clock:                   probe.ClockConfig{Mode: probe.ClockModeBudget, ErrorBudgetMs: 1},
		RTTSamples:              3,
		SampleIntervalSeconds:   1,
		OutputDirectory:         out,
	}
	cfg.ApplyDefaults()
	for _, m := range mods {
		m(cfg)
	}
	r, err := probe.New(cfg, probe.Options{Logger: quietLogger()})
	require.NoError(t, err)
	h := &probeHandle{r: r, err: make(chan error, 1)}
	go func() { h.err <- r.Run(context.Background()) }()
	select {
	case <-r.Started():
	case err := <-h.err:
		t.Fatalf("probe %s ended before arming: %v", id, err)
	case <-time.After(10 * time.Second):
		t.Fatalf("probe %s never armed", id)
	}
	return h
}

func (h *probeHandle) wait(t *testing.T) string {
	t.Helper()
	select {
	case err := <-h.err:
		require.NoError(t, err)
	case <-time.After(20 * time.Second):
		t.Fatal("probe did not finish")
	}
	return h.r.Dir()
}

type network struct {
	ref, a, b *mocknode.Node
}

func newNetwork(t *testing.T) *network {
	ref := mocknode.New(1, 8, 1)
	n := &network{ref: ref, a: ref.Peer(), b: ref.Peer()}
	t.Cleanup(func() { n.a.Close(); n.b.Close(); n.ref.Close() })
	return n
}

// produce mines a block and exposes it on a after aDelay and on b after
// bDelay (both from mining), then on the reference.
func (n *network) produce(txs, logsPerTx int, aDelay, bDelay time.Duration, bVis mocknode.Visibility) *mocknode.Block {
	blk := n.ref.Mine(txs, logsPerTx, 1)
	start := time.Now()
	type step struct {
		at   time.Duration
		node *mocknode.Node
		vis  mocknode.Visibility
	}
	steps := []step{{aDelay, n.a, mocknode.All()}, {bDelay, n.b, bVis}}
	if bDelay < aDelay {
		steps[0], steps[1] = steps[1], steps[0]
	}
	for _, s := range steps {
		time.Sleep(time.Until(start.Add(s.at)))
		s.node.Publish(blk.Number, s.vis)
	}
	n.ref.Publish(blk.Number, mocknode.All())
	time.Sleep(60 * time.Millisecond)
	return blk
}

func reviewConfig(n *network, dirs []string, out string) *Config {
	cfg := &Config{
		Reference:       ReferenceConfig{RPCURL: n.ref.URL()},
		Probes:          dirs,
		HoldConstant:    []string{"cl"},
		MarginMs:        5,
		OutputDirectory: out,
	}
	cfg.ApplyDefaults()
	return cfg
}

func TestReviewEndToEndAndOfflineRegeneration(t *testing.T) {
	n := newNetwork(t)
	out := t.TempDir()
	probes := map[string]bool{schema.ProbeStateNumber: true, schema.ProbeLogsNumber: true, schema.ProbeLogsHash: true}
	pa := startProbe(t, n.a, "pair-a", "host-1", 3, probes, out)
	pb := startProbe(t, n.b, "pair-b", "host-1", 3, probes, out)

	for i := 0; i < 3; i++ {
		n.produce(2, 2, 20*time.Millisecond, 120*time.Millisecond, mocknode.All())
	}
	dirA, dirB := pa.wait(t), pb.wait(t)

	revDir := filepath.Join(out, "review")
	res, err := Run(context.Background(), reviewConfig(n, []string{dirA, dirB}, revDir), Options{Logger: quietLogger()})
	require.NoError(t, err)
	require.Equal(t, 3, res.Summary.Blocks[RefVerified])

	st := res.Summary.Results[schema.ProbeStateNumber]
	require.Equal(t, 3, st.Pairs["pair-a"].Status[OutMatched])
	require.Equal(t, 3, st.Pairs["pair-b"].Status[OutMatched])
	var all *Comparison
	for _, c := range st.Comparisons {
		if c.Group == "all" {
			all = c
		}
	}
	require.NotNil(t, all)
	require.False(t, all.CrossHost)
	require.Equal(t, 3, all.Compared)
	require.Equal(t, 3, all.ObservedAWins, "pair-a received every block 100 ms earlier")
	require.Equal(t, 3, all.InferredAWins)
	require.Len(t, st.Comparisons, 2, "all + cl=mockcl")

	require.NotNil(t, res.Summary.Results[schema.ProbeLogsNumber].Pairs["pair-a"].LagVsState)
	require.Nil(t, st.Pairs["pair-a"].LagVsState, "state_number has no lag against itself")

	for _, br := range res.Blocks {
		o := br.Results["pair-a"][schema.ProbeLogsHash]
		require.Equal(t, OutMatched, o.Status)
		require.True(t, o.LeftCensored, "hash-addressed probes start after the data is known to exist")
		sa := br.Results["pair-a"][schema.ProbeStateNumber]
		require.False(t, sa.LeftCensored)
		require.NotNil(t, sa.LowerMs)
		require.LessOrEqual(t, *sa.LowerMs, *sa.FreshnessMs)
	}

	first, err := os.ReadFile(filepath.Join(revDir, schema.SummaryFile))
	require.NoError(t, err)
	firstBlocks, err := os.ReadFile(filepath.Join(revDir, schema.BlockResultsFile))
	require.NoError(t, err)
	report, err := os.ReadFile(filepath.Join(revDir, schema.ReportFile))
	require.NoError(t, err)
	require.Contains(t, string(report), "## state_number")

	cfg := reviewConfig(n, []string{dirA, dirB}, revDir)
	cfg.Reference.RPCURL = ""
	_, err = Run(context.Background(), cfg, Options{Offline: true, Logger: quietLogger()})
	require.NoError(t, err)
	second, _ := os.ReadFile(filepath.Join(revDir, schema.SummaryFile))
	secondBlocks, _ := os.ReadFile(filepath.Join(revDir, schema.BlockResultsFile))
	require.Equal(t, string(first), string(second), "offline regeneration must reproduce summary.json")
	require.Equal(t, string(firstBlocks), string(secondBlocks))
}

func TestIncompleteLogsNeverCountAsSuccess(t *testing.T) {
	n := newNetwork(t)
	out := t.TempDir()
	probes := map[string]bool{schema.ProbeStateNumber: true, schema.ProbeLogsNumber: true}
	pa := startProbe(t, n.a, "pair-a", "host-1", 1, probes, out)
	pb := startProbe(t, n.b, "pair-b", "host-1", 1, probes, out)
	n.produce(3, 2, 10*time.Millisecond, 10*time.Millisecond, mocknode.Visibility{State: true, Header: true, PartialLogs: 3})
	dirA, dirB := pa.wait(t), pb.wait(t)

	res, err := Run(context.Background(), reviewConfig(n, []string{dirA, dirB}, filepath.Join(out, "review")), Options{Logger: quietLogger()})
	require.NoError(t, err)
	logs := res.Summary.Results[schema.ProbeLogsNumber]
	require.Equal(t, 1, logs.Pairs["pair-a"].Status[OutMatched])
	require.Equal(t, 1, logs.Pairs["pair-b"].Status[OutIncorrect])
	require.Equal(t, 1, logs.Pairs["pair-b"].Denominator, "failures stay in the denominator")
	require.Equal(t, 0.0, logs.Pairs["pair-b"].Availability["slot"])
	require.Equal(t, 1, logs.Comparisons[0].CoverageAWins)
	require.Nil(t, logs.Comparisons[0].Delta, "no finite delta against a failure")
}

func TestOrphanedAndUnverifiedBlocks(t *testing.T) {
	n := newNetwork(t)
	out := t.TempDir()
	probes := map[string]bool{schema.ProbeStateNumber: true, schema.ProbeLogsNumber: true}
	pa := startProbe(t, n.a, "pair-a", "host-1", 1, probes, out)
	blk := n.produce(1, 1, 10*time.Millisecond, 10*time.Millisecond, mocknode.All())
	dirA := pa.wait(t)
	n.ref.Replace(blk.Number, 2, 1)

	res, err := Run(context.Background(), reviewConfig(n, []string{dirA}, filepath.Join(out, "review")), Options{Logger: quietLogger()})
	require.NoError(t, err)
	require.Equal(t, 1, res.Summary.Blocks[RefOrphaned])
	require.Equal(t, OutOrphaned, res.Blocks[0].Results["pair-a"][schema.ProbeStateNumber].Status)
	require.Equal(t, 0, res.Summary.Results[schema.ProbeStateNumber].Pairs["pair-a"].Status[OutMatched])

	cfg := reviewConfig(n, []string{dirA}, filepath.Join(out, "empty-cache"))
	_, err = Run(context.Background(), &Config{Probes: cfg.Probes, OutputDirectory: cfg.OutputDirectory, SchemaVersion: schema.Version,
		MarginMs: 5, DeadlinesMs: cfg.DeadlinesMs, TimelineBlocks: 1}, Options{Offline: true, Logger: quietLogger()})
	require.NoError(t, err)
	var sum Summary
	require.NoError(t, readJSON(filepath.Join(out, "empty-cache", schema.SummaryFile), &sum))
	require.Equal(t, 1, sum.Blocks[RefUnverified])
	require.Equal(t, 0, sum.Results[schema.ProbeStateNumber].Pairs["pair-a"].Status[OutMatched], "unverified data never counts as success")
}

func TestCrossHostMarginAbsorbsClockError(t *testing.T) {
	n := newNetwork(t)
	out := t.TempDir()
	probes := map[string]bool{schema.ProbeStateNumber: true}
	pa := startProbe(t, n.a, "pair-a", "host-1", 2, probes, out)
	pb := startProbe(t, n.b, "pair-b", "host-2", 2, probes, out)
	for i := 0; i < 2; i++ {
		n.produce(1, 1, 10*time.Millisecond, 14*time.Millisecond, mocknode.All())
	}
	dirA, dirB := pa.wait(t), pb.wait(t)

	cfg := reviewConfig(n, []string{dirA, dirB}, filepath.Join(out, "review"))
	cfg.MarginMs = 1
	res, err := Run(context.Background(), cfg, Options{Logger: quietLogger()})
	require.NoError(t, err)
	c := res.Summary.Results[schema.ProbeStateNumber].Comparisons[0]
	require.True(t, c.CrossHost)
	require.Equal(t, 2.0, c.MarginMs, "margin is raised to the combined 1 ms + 1 ms clock budgets")
	require.Equal(t, 2, c.Compared)
	require.Equal(t, 0, c.ObservedBWins)
}

func TestCLSplitAndEpochFlag(t *testing.T) {
	n := newNetwork(t)
	out := t.TempDir()
	probes := map[string]bool{schema.ProbeStateNumber: true}
	withBeacon := func(node *mocknode.Node) func(*probe.Config) {
		return func(c *probe.Config) { c.Pair.CL.BeaconURL = node.BeaconURL() }
	}
	pa := startProbe(t, n.a, "pair-a", "host-1", 3, probes, out, withBeacon(n.a))
	pb := startProbe(t, n.b, "pair-b", "host-1", 3, probes, out, withBeacon(n.b))
	for i := 0; i < 3; i++ {
		n.produce(1, 1, 10*time.Millisecond, 80*time.Millisecond, mocknode.All())
	}
	dirA, dirB := pa.wait(t), pb.wait(t)

	res, err := Run(context.Background(), reviewConfig(n, []string{dirA, dirB}, filepath.Join(out, "review")), Options{Logger: quietLogger()})
	require.NoError(t, err)
	st := res.Summary.Results[schema.ProbeStateNumber]
	for _, id := range []string{"pair-a", "pair-b"} {
		sp := st.Pairs[id].CL["block_gossip"]
		require.NotNil(t, sp, id)
		require.Equal(t, 3, sp.Blocks)
		require.NotNil(t, sp.ReadyAfter)
	}
	p := st.Comparisons[0].CL["block_gossip"]
	require.NotNil(t, p)
	require.Equal(t, 3, p.Compared)
	require.Less(t, p.MilestoneDelta.P50, -30.0, "pair-a's CL saw every block ~70 ms earlier")
	require.Less(t, math.Abs(p.ReadyAfterDelta.P50), 30.0, "after the milestone both pairs are equally fast")

	report, err := os.ReadFile(filepath.Join(out, "review", schema.ReportFile))
	require.NoError(t, err)
	require.Contains(t, string(report), "### Split at CL milestones")
}

func TestEpochBoundary(t *testing.T) {
	slot := func(v uint64) *uint64 { return &v }
	require.True(t, epochBoundary(&BlockResult{Slot: slot(15287680)}, 32))
	require.False(t, epochBoundary(&BlockResult{Slot: slot(15287681)}, 32))
	require.True(t, epochBoundary(&BlockResult{Timeline: map[string]*Timeline{"a": {EpochTransition: true}}}, 0),
		"without a known epoch length the head event's flag is used")
	require.False(t, epochBoundary(&BlockResult{}, 0))
}

func TestSameCL(t *testing.T) {
	run := func(labels map[string]string, version string) *ProbeRun {
		return &ProbeRun{Manifest: schema.Manifest{Labels: labels, CLClientVersion: version}}
	}
	lh := "Lighthouse/v8.2.2-e423a66/x86_64-linux"
	require.True(t, sameCL(run(map[string]string{"cl": "lighthouse"}, ""), run(map[string]string{"cl": "Lighthouse"}, "")))
	require.False(t, sameCL(run(map[string]string{"cl": "lighthouse"}, lh), run(map[string]string{"cl": "caplin"}, lh)), "labels win over versions")
	require.True(t, sameCL(run(nil, lh), run(nil, "Lighthouse/v8.1.0")))
	require.False(t, sameCL(run(nil, lh), run(nil, "Caplin/3.6.1-0c4d9c91 linux/amd64")))
	require.False(t, sameCL(run(nil, ""), run(nil, "")), "unknown CLs are not assumed equal")
}

func TestContextSplitAndMissingMilestones(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	outs := []*Outcome{
		{Status: OutMatched, FreshnessMs: f(2000)},
		{Status: OutMatched, FreshnessMs: f(5000)},
		{Status: OutMatched, FreshnessMs: f(2100)},
		{Status: OutTimeout},
	}
	tls := []*Timeline{
		{CL: []CLMark{{Topic: "block_gossip", Ms: 1600}, {Topic: "head", Ms: 1900}}},
		{CL: []CLMark{{Topic: "block_gossip", Ms: 1700}, {Topic: "head", Ms: 4000}}, EpochTransition: true},
		{CL: []CLMark{{Topic: "block_gossip", Ms: 1650}, {Topic: "block", Ms: 2050}}, Optimistic: true},
		nil,
	}
	epochs := []bool{false, true, false, false}

	st := &PairStats{}
	st.addContext(outs, tls, epochs)
	require.Equal(t, 1, st.OptimisticImports)
	require.Equal(t, 1, st.FreshnessEpoch.N)
	require.Equal(t, 5000.0, st.FreshnessEpoch.P50)
	require.Equal(t, 2, st.FreshnessNonEpoch.N)

	split := pairCLSplit(outs, tls)
	require.Equal(t, 3, split["block_gossip"].Blocks)
	require.Equal(t, 1, split["block_gossip"].Missing, "the timed-out block had no events")
	require.Equal(t, 2, split["head"].Blocks)
	require.Equal(t, 2, split["head"].Missing, "optimistic import emitted no head")
}
