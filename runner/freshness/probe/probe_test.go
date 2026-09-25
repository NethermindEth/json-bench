package probe

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/freshness/mocknode"
	"github.com/jsonrpc-bench/runner/freshness/schema"
)

func testConfig(t *testing.T, node *mocknode.Node, blocks int) *Config {
	t.Helper()
	cfg := &Config{
		Pair: PairConfig{ID: "mock-pair", HostID: "test-host", Labels: map[string]string{"el": "mock"},
			EL: ELConfig{URL: node.URL()}},
		Chain:                   ChainConfig{SlotDurationSeconds: 1},
		BlockCount:              blocks,
		MaxRunDurationSeconds:   20,
		PollIntervalMs:          2,
		LookaheadPollIntervalMs: 10,
		HeadWatchIntervalMs:     50,
		RequestTimeoutMs:        500,
		LogsStablePolls:         3,
		Clock:                   ClockConfig{Mode: ClockModeBudget, ErrorBudgetMs: 1},
		RTTSamples:              3,
		SampleIntervalSeconds:   1,
		OutputDirectory:         t.TempDir(),
	}
	cfg.ApplyDefaults()
	return cfg
}

type run struct {
	r    *Runner
	err  chan error
	stop context.CancelFunc
}

func start(t *testing.T, cfg *Config) *run {
	t.Helper()
	log := logrus.New()
	log.SetLevel(logrus.WarnLevel)
	r, err := New(cfg, Options{Logger: log})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	out := &run{r: r, err: make(chan error, 1), stop: cancel}
	go func() { out.err <- r.Run(ctx) }()
	select {
	case <-r.Started():
	case err := <-out.err:
		t.Fatalf("run ended before arming: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("probe never armed")
	}
	return out
}

func (x *run) wait(t *testing.T) {
	t.Helper()
	select {
	case err := <-x.err:
		require.NoError(t, err)
	case <-time.After(20 * time.Second):
		x.stop()
		t.Fatal("probe did not finish")
	}
}

func readJSONL[T any](t *testing.T, path string) []T {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	var out []T
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var v T
		require.NoError(t, json.Unmarshal(sc.Bytes(), &v))
		out = append(out, v)
	}
	return out
}

func targetsByNumber(t *testing.T, dir string) map[uint64]schema.Target {
	out := map[uint64]schema.Target{}
	for _, tg := range readJSONL[schema.Target](t, filepath.Join(dir, schema.TargetsFile)) {
		out[tg.BlockNumber] = tg
	}
	return out
}

func pause() { time.Sleep(40 * time.Millisecond) }

func TestStateBeforeLogsAndPartialLogs(t *testing.T) {
	node := mocknode.New(1, 8, 1)
	defer node.Close()
	x := start(t, testConfig(t, node, 2))

	var mined []*mocknode.Block
	for i := 0; i < 2; i++ {
		b := node.Mine(3, 2, 1)
		mined = append(mined, b)
		pause()
		node.Publish(b.Number, mocknode.Visibility{State: true})
		pause()
		node.Publish(b.Number, mocknode.Visibility{State: true, Header: true, PartialLogs: 2})
		pause()
		node.Publish(b.Number, mocknode.All())
		pause()
	}
	x.wait(t)

	targets := targetsByNumber(t, x.r.Dir())
	require.Len(t, targets, 2)
	for _, b := range mined {
		tg := targets[b.Number]
		require.Equal(t, b.Hash.Hex(), strings.ToLower(tg.BlockHash))
		st, lg := tg.Probes[schema.ProbeStateNumber], tg.Probes[schema.ProbeLogsNumber]
		require.Equal(t, schema.StatusMatched, st.Status)
		require.Equal(t, schema.StatusMatched, lg.Status, lg.Reason)
		require.Less(t, int64(st.MatchReceived.Wall), int64(lg.MatchReceived.Wall), "state must be ready before logs")
		require.False(t, st.LeftCensored)
		require.False(t, lg.LeftCensored)
	}

	attempts := readJSONL[schema.Attempt](t, filepath.Join(x.r.Dir(), schema.AttemptsFile))
	var sawPartial, sawNotReadyBeforeMatch bool
	for _, a := range attempts {
		if a.Probe == schema.ProbeLogsNumber && a.Local == schema.LocalMismatch {
			sawPartial = true
		}
		if a.Probe == schema.ProbeStateNumber && a.Class == schema.ClassNotReady && a.Edge == schema.EdgeLast {
			sawNotReadyBeforeMatch = true
		}
	}
	require.True(t, sawPartial, "partial log sets must be recorded as local mismatches")
	require.True(t, sawNotReadyBeforeMatch, "the last not-ready attempt must be kept as the lower bound")
	require.Less(t, len(attempts), 100, "transition logging must not record every poll")

	var m schema.Manifest
	raw, err := os.ReadFile(filepath.Join(x.r.Dir(), schema.ManifestFile))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &m))
	require.Equal(t, schema.OutcomeCompleted, m.Outcome)
	require.Equal(t, "user_budget", m.Clock.Source)
	require.NotNil(t, m.RTTStart)
}

func TestEmptyBlocksAreNotApplicable(t *testing.T) {
	node := mocknode.New(1, 8, 1)
	defer node.Close()
	cfg := testConfig(t, node, 2)
	cfg.Probes[schema.ProbeBlockReceipts] = true
	x := start(t, cfg)

	noTx := node.Mine(0, 0, 1)
	pause()
	node.Publish(noTx.Number, mocknode.All())
	pause()
	noLogs := node.Mine(2, 0, 1)
	pause()
	node.Publish(noLogs.Number, mocknode.All())
	x.wait(t)

	targets := targetsByNumber(t, x.r.Dir())
	a := targets[noTx.Number]
	require.Equal(t, schema.StatusNotApplicable, a.Probes[schema.ProbeLogsNumber].Status)
	require.Equal(t, schema.ReasonEmptyLogs, a.Probes[schema.ProbeLogsNumber].Reason)
	require.Equal(t, schema.StatusNotApplicable, a.Probes[schema.ProbeBlockReceipts].Status)
	require.Equal(t, schema.StatusMatched, a.Probes[schema.ProbeStateNumber].Status)

	b := targets[noLogs.Number]
	require.Equal(t, schema.StatusNotApplicable, b.Probes[schema.ProbeLogsNumber].Status)
	require.Equal(t, schema.StatusMatched, b.Probes[schema.ProbeBlockReceipts].Status, b.Probes[schema.ProbeBlockReceipts].Reason)
}

func TestMissedSlotKeepsTargetAndPartialLogsHitDeadline(t *testing.T) {
	node := mocknode.New(1, 8, 1)
	defer node.Close()
	x := start(t, testConfig(t, node, 1))

	b := node.Mine(2, 1, 3)
	time.Sleep(300 * time.Millisecond)
	node.Publish(b.Number, mocknode.Visibility{State: true, Header: true, PartialLogs: 1})
	x.wait(t)

	tg := targetsByNumber(t, x.r.Dir())[b.Number]
	require.Equal(t, 2, tg.MissedSlots)
	require.Equal(t, schema.StatusMatched, tg.Probes[schema.ProbeStateNumber].Status)
	lg := tg.Probes[schema.ProbeLogsNumber]
	require.Equal(t, schema.StatusDeadlineExceeded, lg.Status)
	require.Contains(t, lg.Reason, "bloom")

	events := readJSONL[schema.Event](t, filepath.Join(x.r.Dir(), schema.EventsFile))
	found := false
	for _, e := range events {
		if e.Type == schema.EventMissedSlot && e.BlockNumber != nil && *e.BlockNumber == b.Number {
			found = true
		}
	}
	require.True(t, found)
}

func TestNotReadySignaturesAcrossStyles(t *testing.T) {
	styles := map[string]mocknode.NotReadyStyle{
		"http500":    {Code: -32603, Message: "Internal error: block #19999 not available", HTTPStatus: 500},
		"emptyLogs":  {Code: -32001, Message: "resource not found", EmptyLogs: true},
		"stringCode": {Code: 39001, Message: "Unknown block 0x1a2b"},
	}
	for name, style := range styles {
		t.Run(name, func(t *testing.T) {
			node := mocknode.New(1, 8, 1)
			defer node.Close()
			node.SetStyle(style)
			x := start(t, testConfig(t, node, 1))
			b := node.Mine(1, 1, 1)
			pause()
			node.Publish(b.Number, mocknode.All())
			x.wait(t)

			attempts := readJSONL[schema.Attempt](t, filepath.Join(x.r.Dir(), schema.AttemptsFile))
			for _, p := range []string{schema.ProbeStateNumber, schema.ProbeLogsNumber} {
				var notReady, strong bool
				for _, a := range attempts {
					if a.Probe == p && a.Class == schema.ClassNotReady {
						notReady = true
						strong = strong || !a.WeakMatch
					}
				}
				require.True(t, notReady && strong, "%s: pre-block answers must match the preflight signature", p)
			}
			tg := targetsByNumber(t, x.r.Dir())[b.Number]
			require.Equal(t, schema.StatusMatched, tg.Probes[schema.ProbeLogsNumber].Status)
		})
	}
}

func TestParentMismatchIsFlagged(t *testing.T) {
	node := mocknode.New(1, 8, 1)
	defer node.Close()
	x := start(t, testConfig(t, node, 2))

	first := node.Mine(1, 1, 1)
	pause()
	node.Publish(first.Number, mocknode.All())
	pause()
	pause()
	sibling := node.Replace(first.Number, 2, 1)
	node.Publish(first.Number, mocknode.All())
	second := node.Mine(1, 1, 1)
	require.Equal(t, sibling.Hash, second.ParentHash)
	pause()
	node.Publish(second.Number, mocknode.All())
	x.wait(t)

	tg := targetsByNumber(t, x.r.Dir())[second.Number]
	require.True(t, tg.ParentMismatch)
	require.Equal(t, strings.ToLower(sibling.Hash.Hex()), tg.ParentHash)
	require.Equal(t, schema.StatusMatched, tg.Probes[schema.ProbeStateNumber].Status)
}

func TestSlowNodeSkipsPollsInsteadOfQueueing(t *testing.T) {
	node := mocknode.New(1, 8, 1)
	defer node.Close()
	node.SetDelay("eth_call", 30*time.Millisecond)
	x := start(t, testConfig(t, node, 1))
	b := node.Mine(1, 1, 1)
	time.Sleep(200 * time.Millisecond)
	node.Publish(b.Number, mocknode.All())
	x.wait(t)

	st := targetsByNumber(t, x.r.Dir())[b.Number].Probes[schema.ProbeStateNumber]
	require.Equal(t, schema.StatusMatched, st.Status)
	require.Greater(t, st.SkippedPolls, 0)
}

func TestJumpAheadMarksLateArmed(t *testing.T) {
	node := mocknode.New(1, 8, 1)
	defer node.Close()
	node.SetDelay("eth_call", 150*time.Millisecond)
	node.SetDelay("eth_getLogs", 150*time.Millisecond)
	x := start(t, testConfig(t, node, 6))
	var blocks []*mocknode.Block
	for i := 0; i < 6; i++ {
		blocks = append(blocks, node.Mine(1, 1, 1))
	}
	for _, b := range blocks {
		node.Publish(b.Number, mocknode.All())
	}
	x.wait(t)

	targets := targetsByNumber(t, x.r.Dir())
	require.Len(t, targets, 6)
	late := 0
	for _, tg := range targets {
		if tg.LateArmed {
			late++
			require.Equal(t, schema.StatusLateArmed, tg.Probes[schema.ProbeStateNumber].Status)
			require.Nil(t, tg.Probes[schema.ProbeStateNumber].MatchReceived)
		}
	}
	require.Greater(t, late, 0)
}

func TestPreflightRejectsSyncingNode(t *testing.T) {
	node := mocknode.New(1, 8, 1)
	defer node.Close()
	node.SetSyncing(true)
	r, err := New(testConfig(t, node, 1), Options{Logger: logrus.New()})
	require.NoError(t, err)
	err = r.Run(context.Background())
	require.ErrorContains(t, err, "syncing")
}

func TestBeaconEventsAndSlots(t *testing.T) {
	node := mocknode.New(1, 8, 1)
	defer node.Close()
	cfg := testConfig(t, node, 1)
	cfg.Chain.SlotDurationSeconds = 0
	cfg.Pair.CL.BeaconURL = node.BeaconURL()
	x := start(t, cfg)
	b := node.Mine(1, 1, 1)
	pause()
	node.Publish(b.Number, mocknode.All())
	x.wait(t)

	tg := targetsByNumber(t, x.r.Dir())[b.Number]
	require.NotNil(t, tg.Slot)
	require.Equal(t, (b.Timestamp-node.Genesis())/1, *tg.Slot)
	var m schema.Manifest
	raw, _ := os.ReadFile(filepath.Join(x.r.Dir(), schema.ManifestFile))
	require.NoError(t, json.Unmarshal(raw, &m))
	require.Equal(t, "beacon_spec", m.SlotDurationSource)
	require.Equal(t, "mockbeacon/v1.0.0", m.CLClientVersion)
}

func TestRedactedConfigHasNoSecrets(t *testing.T) {
	cfg := &Config{Pair: PairConfig{ID: "x", EL: ELConfig{URL: "https://user:pw@rpc.example.com/v3/SECRETKEY?x=1", Headers: map[string]string{"Authorization": "Bearer SECRET"}}}}
	red := cfg.Redacted()
	b, _ := json.Marshal(red)
	require.NotContains(t, string(b), "SECRET")
	require.NotContains(t, string(b), "pw")
	require.Contains(t, string(b), "rpc.example.com")
}

func TestMaxDurationStillWritesOpenTargets(t *testing.T) {
	node := mocknode.New(1, 8, 1)
	defer node.Close()
	cfg := testConfig(t, node, 5)
	cfg.MaxRunDurationSeconds = 1
	cfg.Probes = map[string]bool{schema.ProbeStateNumber: true}
	x := start(t, cfg)
	b := node.Mine(1, 1, 1)
	node.Publish(b.Number, mocknode.Visibility{State: true})
	x.wait(t)

	tg, ok := targetsByNumber(t, x.r.Dir())[b.Number]
	require.True(t, ok, "a target whose header never arrived must still be written")
	require.Equal(t, schema.StatusMatched, tg.Probes[schema.ProbeStateNumber].Status)
	require.Empty(t, tg.BlockHash)

	var m schema.Manifest
	raw, err := os.ReadFile(filepath.Join(x.r.Dir(), schema.ManifestFile))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &m))
	require.Equal(t, schema.OutcomeMaxDuration, m.Outcome)
}

func TestImplausibleSlotDurationIsRejected(t *testing.T) {
	node := mocknode.New(1, 8, 1)
	defer node.Close()
	cfg := testConfig(t, node, 1)
	cfg.Chain.SlotDurationSeconds = 1 << 40
	r, err := New(cfg, Options{Logger: logrus.New()})
	require.NoError(t, err)
	require.ErrorContains(t, r.Run(context.Background()), "implausible")
}
