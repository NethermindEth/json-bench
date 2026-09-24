package probe

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/jsonrpc-bench/runner/freshness/capture"
	"github.com/jsonrpc-bench/runner/freshness/ethrpc"
	"github.com/jsonrpc-bench/runner/freshness/hostinfo"
	"github.com/jsonrpc-bench/runner/freshness/schema"
)

const recordBuffer = 1 << 16

// Options are run-time switches that are not part of the measured workload.
type Options struct {
	RecordAllAttempts bool
	Logger            logrus.FieldLogger
}

// Runner executes one probe run. All target state is guarded by mu; workers
// only touch their own counters outside it.
type Runner struct {
	cfg  *Config
	opts Options
	log  logrus.FieldLogger

	clock  *schema.Clock
	el     *ethrpc.Client
	beacon *beaconClient
	env    schema.Envelope
	dir    string

	events    *capture.JSONL
	targetsW  *capture.JSONL
	attempts  *capture.JSONL
	responses *capture.Store

	manifest *schema.Manifest
	caps     *schema.Capabilities
	probes   []string
	sigs     map[string]*schema.NotReadySignature
	slotDur  time.Duration

	mu            sync.Mutex
	targets       map[uint64]*target
	headers       map[uint64]*ethrpc.Header
	firstBlock    uint64
	lastBlock     uint64
	lastConfirmed uint64
	lastProgress  time.Time
	stallReported bool
	finished      int
	allDone       chan struct{}
	started       chan struct{}
	wg            sync.WaitGroup
}

// target is one block number being measured. Channels are closed exactly
// once, under Runner.mu.
type target struct {
	number    uint64
	ctx       context.Context
	cancel    context.CancelFunc
	rec       *schema.Target
	fast      bool
	promoted  chan struct{}
	headerCh  chan struct{}
	header    *ethrpc.Header
	parent    *common.Hash
	fetching  bool
	running   int
	lateArmed bool
}

func New(cfg *Config, opts Options) (*Runner, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	log := opts.Logger
	if log == nil {
		log = logrus.StandardLogger()
	}
	clock := schema.NewClock()
	runID := time.Now().UTC().Format("20060102T150405Z") + "-" + uuid.NewString()[:8]
	r := &Runner{
		cfg:     cfg,
		opts:    opts,
		log:     log.WithField("pair", cfg.Pair.ID),
		clock:   clock,
		sigs:    map[string]*schema.NotReadySignature{},
		targets: map[uint64]*target{},
		headers: map[uint64]*ethrpc.Header{},
		allDone: make(chan struct{}),
		started: make(chan struct{}),
		env: schema.Envelope{
			SchemaVersion: schema.Version,
			RunID:         runID,
			PairID:        cfg.Pair.ID,
			HostID:        cfg.Pair.HostID,
			ClockDomain:   cfg.Pair.HostID,
		},
	}
	timeout := time.Duration(cfg.RequestTimeoutMs) * time.Millisecond
	r.el = ethrpc.NewClient(ethrpc.Options{URL: cfg.Pair.EL.URL, Headers: cfg.Pair.EL.Headers, Timeout: timeout}, clock)
	if cfg.Pair.CL.BeaconURL != "" {
		r.beacon = newBeaconClient(cfg.Pair.CL, 5*time.Second)
	}
	hostname, _ := os.Hostname()
	r.manifest = &schema.Manifest{
		SchemaVersion:     schema.Version,
		RunID:             runID,
		PairID:            cfg.Pair.ID,
		HostID:            cfg.Pair.HostID,
		Hostname:          hostname,
		ClockDomain:       cfg.Pair.HostID,
		Labels:            cfg.Pair.Labels,
		Config:            cfg.Redacted(),
		WarmupBlocks:      cfg.WarmupBlocks,
		RecordAllAttempts: opts.RecordAllAttempts,
		Dropped:           map[string]int64{},
		Counts:            map[string]int{},
	}
	return r, nil
}

// Dir is the run's output directory (valid after Run starts).
func (r *Runner) Dir() string { return r.dir }

// Started is closed once the first targets are armed.
func (r *Runner) Started() <-chan struct{} { return r.started }

// Run executes preflight and the measurement. The output directory is always
// finalised, including on error, so partial runs remain reviewable.
func (r *Runner) Run(ctx context.Context) (err error) {
	r.manifest.StartedAt = r.clock.Now()
	r.dir = filepath.Join(r.cfg.OutputDirectory, r.cfg.Pair.ID+"-"+r.env.RunID)
	if err := r.openOutputs(); err != nil {
		return err
	}
	r.manifest.Outcome = schema.OutcomeCompleted
	defer func() {
		if err != nil {
			r.manifest.Outcome = schema.OutcomeError
			r.manifest.OutcomeReason = err.Error()
		}
		if cerr := r.finalize(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	r.log.Infof("preflight against %s", RedactURL(r.cfg.Pair.EL.URL))
	if err := r.preflight(ctx); err != nil {
		return fmt.Errorf("preflight: %w", err)
	}
	if err := capture.WriteJSON(filepath.Join(r.dir, schema.CapabilitiesFile), r.caps); err != nil {
		return err
	}
	r.readClock(ctx, true)
	r.manifest.RTTStart = hostinfo.MeasureRTT(ctx, r.el, r.cfg.RTTSamples)

	parent, err := r.waitForStart(ctx)
	if err != nil {
		return err
	}
	r.firstBlock = parent.Number + 1
	r.lastBlock = r.firstBlock + uint64(r.cfg.WarmupBlocks+r.cfg.BlockCount) - 1
	r.manifest.FirstBlock, r.manifest.LastBlock = r.firstBlock, r.lastBlock
	r.event(schema.Event{Type: schema.EventRunStart, Source: "probe", Data: map[string]any{
		"first_block": r.firstBlock, "last_block": r.lastBlock, "probes": r.probes,
		"slot_duration_seconds": r.manifest.SlotDurationSeconds,
	}})
	r.log.Infof("measuring blocks %d..%d (%d warm-up) with probes %s", r.firstBlock, r.lastBlock, r.cfg.WarmupBlocks, strings.Join(r.probes, ","))

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(r.cfg.MaxRunDurationSeconds)*time.Second)
	defer cancel()

	var bg sync.WaitGroup
	bgCtx, stopBG := context.WithCancel(runCtx)
	bg.Add(2)
	go func() { defer bg.Done(); r.watchHead(bgCtx) }()
	go func() { defer bg.Done(); r.sampleHost(bgCtx) }()
	if r.beacon != nil && (r.cfg.Pair.CL.Events == nil || *r.cfg.Pair.CL.Events) {
		bg.Add(1)
		go func() { defer bg.Done(); r.followBeacon(bgCtx) }()
	}

	r.mu.Lock()
	r.headers[parent.Number] = parent
	r.lastConfirmed = parent.Number
	r.lastProgress = time.Now()
	r.armLocked(r.firstBlock, true, &parent.Hash)
	r.armLocked(r.firstBlock+1, false, nil)
	r.mu.Unlock()
	close(r.started)

	select {
	case <-r.allDone:
	case <-runCtx.Done():
		switch {
		case errors.Is(ctx.Err(), context.Canceled):
			r.manifest.Outcome = schema.OutcomeInterrupted
		case errors.Is(runCtx.Err(), context.DeadlineExceeded):
			r.manifest.Outcome = schema.OutcomeMaxDuration
		}
	}

	stopBG()
	bg.Wait()
	r.mu.Lock()
	for _, t := range r.targets {
		t.cancel()
	}
	r.mu.Unlock()
	r.wg.Wait()
	// Targets whose probes all finished while the header was still unknown
	// have no worker left to close them.
	r.mu.Lock()
	for _, t := range r.targets {
		r.maybeFinishLocked(t)
	}
	r.mu.Unlock()

	endCtx, endCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer endCancel()
	r.manifest.RTTEnd = hostinfo.MeasureRTT(endCtx, r.el, r.cfg.RTTSamples)
	r.event(schema.Event{Type: schema.EventRunEnd, Source: "probe", Status: r.manifest.Outcome})
	return nil
}

func (r *Runner) openOutputs() error {
	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		return err
	}
	var err error
	if r.events, err = capture.NewJSONL(filepath.Join(r.dir, schema.EventsFile), recordBuffer); err != nil {
		return err
	}
	if r.targetsW, err = capture.NewJSONL(filepath.Join(r.dir, schema.TargetsFile), recordBuffer); err != nil {
		return err
	}
	if r.attempts, err = capture.NewJSONL(filepath.Join(r.dir, schema.AttemptsFile), recordBuffer); err != nil {
		return err
	}
	r.responses, err = capture.NewStore(filepath.Join(r.dir, schema.ResponsesDir), recordBuffer)
	return err
}

func (r *Runner) finalize() error {
	var errs []error
	for name, w := range map[string]*capture.JSONL{"events": r.events, "targets": r.targetsW, "attempts": r.attempts} {
		if w == nil {
			continue
		}
		r.manifest.Dropped[name] = w.Dropped()
		if err := w.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if r.responses != nil {
		r.manifest.Dropped["responses"] = r.responses.Dropped()
		if err := r.responses.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	for name, n := range r.manifest.Dropped {
		if n > 0 {
			r.log.Warnf("%d %s records dropped (buffer full)", n, name)
		}
	}
	r.manifest.FinishedAt = r.clock.Now()
	if r.caps != nil && r.manifest.ChainID == 0 {
		r.manifest.ChainID = r.caps.ChainID
	}
	if err := capture.WriteJSON(filepath.Join(r.dir, schema.ManifestFile), r.manifest); err != nil {
		errs = append(errs, err)
	}
	r.log.Infof("probe output written to %s (%s)", r.dir, r.manifest.Outcome)
	return errors.Join(errs...)
}

// waitForStart returns the parent of the first measured block.
func (r *Runner) waitForStart(ctx context.Context) (*ethrpc.Header, error) {
	if r.cfg.Start.Time != "" {
		at, _ := time.Parse(time.RFC3339, r.cfg.Start.Time)
		if d := time.Until(at); d > 0 {
			r.log.Infof("waiting %s for start.time %s", d.Round(time.Second), r.cfg.Start.Time)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(d):
			}
		}
	}
	if r.cfg.Start.Block == 0 {
		return r.fetchHeader(ctx, "latest")
	}
	want := r.cfg.Start.Block - 1
	var lastLogged time.Time
	for {
		h, err := r.fetchHeader(ctx, "latest")
		if err != nil {
			return nil, err
		}
		if h.Number > want {
			r.log.Warnf("start.block %d already passed (head %d); starting at %d", r.cfg.Start.Block, h.Number, h.Number+1)
		}
		if h.Number >= want {
			return h, nil
		}
		if time.Since(lastLogged) >= r.slotDur {
			r.log.Infof("waiting for start.block %d (head %d, ~%s)", r.cfg.Start.Block, h.Number,
				(time.Duration(want-h.Number) * r.slotDur).Round(time.Second))
			lastLogged = time.Now()
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(r.watchInterval()):
		}
	}
}

func (r *Runner) watchInterval() time.Duration {
	if r.cfg.HeadWatchIntervalMs > 0 {
		return time.Duration(r.cfg.HeadWatchIntervalMs) * time.Millisecond
	}
	if r.slotDur > 0 {
		return r.slotDur / 4
	}
	return 3 * time.Second
}

// armLocked opens a target unless it already exists or is past the run's
// range. fast selects the full polling rate; otherwise the target is a
// low-rate lookahead until promoted.
func (r *Runner) armLocked(n uint64, fast bool, parent *common.Hash) {
	if n < r.firstBlock || n > r.lastBlock {
		return
	}
	if t, ok := r.targets[n]; ok {
		if fast && !t.fast {
			r.promoteLocked(t, parent)
		}
		return
	}
	if n <= r.lastConfirmed {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	now := r.clock.Now()
	t := &target{
		number:   n,
		ctx:      ctx,
		cancel:   cancel,
		fast:     fast,
		promoted: make(chan struct{}),
		headerCh: make(chan struct{}),
		parent:   parent,
		rec: &schema.Target{
			Envelope:    r.env,
			BlockNumber: n,
			ArmedAt:     now,
			Warmup:      n < r.firstBlock+uint64(r.cfg.WarmupBlocks),
			Probes:      map[string]*schema.ProbeResult{},
		},
	}
	if fast {
		close(t.promoted)
		t.rec.Promoted = &now
	}
	if parent != nil {
		t.rec.ExpectedParent = ethrpc.HashHex(*parent)
	}
	r.targets[n] = t
	r.event(schema.Event{Type: schema.EventArm, BlockNumber: &n, Source: "probe", Status: rateName(fast)})
	for _, p := range r.probes {
		if !hashAddressed(p) {
			r.startWorkerLocked(t, p)
		}
	}
}

func rateName(fast bool) string {
	if fast {
		return "full_rate"
	}
	return "lookahead"
}

func (r *Runner) promoteLocked(t *target, parent *common.Hash) {
	now := r.clock.Now()
	t.fast = true
	t.rec.Promoted = &now
	if parent != nil && t.parent == nil {
		t.parent = parent
		t.rec.ExpectedParent = ethrpc.HashHex(*parent)
	}
	close(t.promoted)
	n := t.number
	r.event(schema.Event{Type: schema.EventPromote, BlockNumber: &n, Source: "probe"})
}

func (r *Runner) startWorkerLocked(t *target, probe string) {
	res := &schema.ProbeResult{ArmedAt: r.clock.Now(), Classes: map[string]int{}}
	t.rec.Probes[probe] = res
	t.running++
	w := &worker{r: r, t: t, probe: probe, res: res, sig: r.sigs[probe], firstSeen: map[string]schema.Stamp{}}
	if t.header != nil {
		w.exp = r.expectationLocked(t)
	}
	if t.parent != nil {
		w.exp.ParentHash = t.parent
	}
	r.wg.Add(1)
	go w.run()
}

func (r *Runner) expectationLocked(t *target) ethrpc.Expectation {
	exp := ethrpc.Expectation{ParentHash: t.parent, Header: t.header}
	if t.header != nil && len(t.header.TxHashes) > 0 {
		tx := t.header.TxHashes[len(t.header.TxHashes)-1]
		exp.TxHash = &tx
	}
	return exp
}

// blockVisible is called when any response shows the node has block n. It
// starts the one header fetch that fixes the target's identity and deadline.
func (r *Runner) blockVisible(t *target) {
	r.mu.Lock()
	if t.fetching || t.header != nil {
		r.mu.Unlock()
		return
	}
	t.fetching = true
	r.mu.Unlock()
	r.wg.Add(1)
	go r.fetchTargetHeader(t, "probe_response")
}

func (r *Runner) fetchTargetHeader(t *target, source string) {
	defer r.wg.Done()
	interval := time.Duration(r.cfg.PollIntervalMs) * time.Millisecond
	for {
		h, err := r.fetchHeader(t.ctx, ethrpc.Quantity(t.number))
		if err == nil && h != nil && h.Number == t.number {
			r.onHeader(t, h, source)
			return
		}
		select {
		case <-t.ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

func (r *Runner) onHeader(t *target, h *ethrpc.Header, source string) {
	observed := r.clock.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	if t.header != nil {
		return
	}
	n := t.number
	t.header = h
	rec := t.rec
	rec.BlockHash = ethrpc.HashHex(h.Hash)
	rec.ParentHash = ethrpc.HashHex(h.ParentHash)
	rec.Timestamp = h.Timestamp
	rec.SlotStart = schema.Nanos(int64(h.Timestamp) * int64(time.Second))
	rec.Deadline = rec.SlotStart + schema.Nanos(r.slotDur)
	rec.TxCount = len(h.TxHashes)
	rec.GasUsed = h.GasUsed
	rec.LogsBloomEmpty = h.LogsBloom == [256]byte{}
	rec.ReceiptsRoot = ethrpc.HashHex(h.ReceiptsRoot)
	rec.HeaderObserved = &observed
	rec.HeaderSource = source
	if g := r.manifest.BeaconGenesisTime; g != nil && h.Timestamp >= *g {
		slot := (h.Timestamp - *g) / r.manifest.SlotDurationSeconds
		rec.Slot = &slot
	}

	if t.parent != nil && *t.parent != h.ParentHash {
		rec.ParentMismatch = true
		r.event(schema.Event{Type: schema.EventReorg, BlockNumber: &n, BlockHash: rec.BlockHash, Source: "probe",
			Reason: "header parent differs from the previously observed block", Data: map[string]any{
				"expected_parent": ethrpc.HashHex(*t.parent), "header_parent": rec.ParentHash}})
	}
	parent := h.ParentHash
	t.parent = &parent

	if prev, ok := r.headers[n-1]; ok && h.Timestamp > prev.Timestamp {
		gap := (h.Timestamp - prev.Timestamp) / r.manifest.SlotDurationSeconds
		if gap > 1 {
			rec.MissedSlots = int(gap - 1)
			r.event(schema.Event{Type: schema.EventMissedSlot, BlockNumber: &n, BlockHash: rec.BlockHash, Source: "execution_header",
				Boundary: "timestamp gap to parent", Data: map[string]any{"missed_slots": rec.MissedSlots}})
		}
	}
	r.event(schema.Event{Type: schema.EventHeader, BlockNumber: &n, BlockHash: rec.BlockHash, Slot: rec.Slot, Source: source,
		Boundary: "eth_getBlockByNumber response complete"})

	r.headers[n] = h
	for k := range r.headers {
		if k+64 < n {
			delete(r.headers, k)
		}
	}
	close(t.headerCh)
	for _, p := range r.probes {
		if hashAddressed(p) {
			r.startWorkerLocked(t, p)
		}
	}

	if n > r.lastConfirmed {
		r.lastConfirmed = n
		r.lastProgress = time.Now()
		r.stallReported = false
		hash := h.Hash
		r.armLocked(n+1, true, &hash)
		r.armLocked(n+2, false, nil)
	}
	r.maybeFinishLocked(t)
}

// workerDone records a finished probe and closes the target when it was the
// last one running.
func (r *Runner) workerDone(t *target) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t.running--
	r.maybeFinishLocked(t)
}

func (r *Runner) maybeFinishLocked(t *target) {
	if t.running > 0 {
		return
	}
	if t.header == nil && t.ctx.Err() == nil {
		return
	}
	if _, ok := r.targets[t.number]; !ok {
		return
	}
	for _, p := range r.probes {
		if _, ok := t.rec.Probes[p]; !ok {
			t.rec.Probes[p] = &schema.ProbeResult{Status: schema.StatusAborted, Reason: "target closed before the header was known"}
		}
	}
	delete(r.targets, t.number)
	t.cancel()
	r.targetsW.Write(t.rec)
	r.finished++
	r.manifest.Counts["targets"]++
	if t.lateArmed {
		r.manifest.Counts["late_armed"]++
	}
	r.logTarget(t)
	if r.lastConfirmed >= r.lastBlock && len(r.targets) == 0 {
		select {
		case <-r.allDone:
		default:
			close(r.allDone)
		}
	}
}

func (r *Runner) logTarget(t *target) {
	rec := t.rec
	parts := make([]string, 0, len(r.probes))
	for _, p := range r.probes {
		res := rec.Probes[p]
		if res.MatchReceived != nil && rec.SlotStart != 0 {
			ms := float64(int64(res.MatchReceived.Wall)-int64(rec.SlotStart)) / 1e6
			parts = append(parts, fmt.Sprintf("%s=%+.0fms", p, ms))
		} else {
			parts = append(parts, p+"="+res.Status)
		}
	}
	hash := rec.BlockHash
	if len(hash) > 12 {
		hash = hash[:12]
	}
	extra := ""
	if rec.MissedSlots > 0 {
		extra = fmt.Sprintf(" missed_slots=%d", rec.MissedSlots)
	}
	if rec.Warmup {
		extra += " warmup"
	}
	r.log.Infof("block %d %s %s%s", rec.BlockNumber, hash, strings.Join(parts, " "), extra)
}

// watchHead is the low-rate safety net: it catches blocks no probe response
// revealed, nodes that jumped past the armed window, and stalls.
func (r *Runner) watchHead(ctx context.Context) {
	ticker := time.NewTicker(r.watchInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		raw, err := r.el.CallResult(ctx, "eth_blockNumber")
		if err != nil {
			continue
		}
		head, err := ethrpc.ParseQuantity(raw)
		if err != nil {
			continue
		}
		r.checkHead(ctx, head)
	}
}

func (r *Runner) checkHead(ctx context.Context, head uint64) {
	r.mu.Lock()
	window := r.lastConfirmed + 2
	var skipped []uint64
	if head > window {
		for n := window + 1; n <= head && n <= r.lastBlock; n++ {
			skipped = append(skipped, n)
		}
	}
	var visible []*target
	for n, t := range r.targets {
		if n <= head && t.header == nil && !t.fetching {
			visible = append(visible, t)
		}
	}
	stalled := !r.stallReported && time.Since(r.lastProgress) > time.Duration(r.cfg.StallSlots)*r.slotDur
	if stalled {
		r.stallReported = true
	}
	lastConfirmed := r.lastConfirmed
	r.mu.Unlock()

	for _, t := range visible {
		r.mu.Lock()
		if t.fetching || t.header != nil {
			r.mu.Unlock()
			continue
		}
		t.fetching = true
		r.mu.Unlock()
		r.wg.Add(1)
		go r.fetchTargetHeader(t, "head_watch")
	}
	if len(skipped) > 0 {
		r.recordLateArmed(ctx, skipped, head)
	}
	if stalled {
		data := map[string]any{"head": head, "last_confirmed": lastConfirmed}
		if raw, err := r.el.CallResult(ctx, "eth_syncing"); err == nil {
			data["eth_syncing"] = string(raw)
		}
		r.event(schema.Event{Type: schema.EventStall, Source: "head_watch", Reason: fmt.Sprintf("no new block for %d slots", r.cfg.StallSlots), Data: data})
		r.log.Warnf("no new block for %d slots (head %d)", r.cfg.StallSlots, head)
	}
}

// recordLateArmed writes identity-only targets for blocks the probe never
// polled before they became available, then re-arms after head. No latency
// is reported for them: the availability transition was not observed.
func (r *Runner) recordLateArmed(ctx context.Context, numbers []uint64, head uint64) {
	for _, n := range numbers {
		h, err := r.fetchHeader(ctx, ethrpc.Quantity(n))
		r.mu.Lock()
		if _, exists := r.targets[n]; exists || n <= r.lastConfirmed {
			r.mu.Unlock()
			continue
		}
		num := n
		rec := &schema.Target{Envelope: r.env, BlockNumber: n, ArmedAt: r.clock.Now(), LateArmed: true,
			Warmup: n < r.firstBlock+uint64(r.cfg.WarmupBlocks), Probes: map[string]*schema.ProbeResult{}}
		if err == nil && h != nil {
			rec.BlockHash = ethrpc.HashHex(h.Hash)
			rec.ParentHash = ethrpc.HashHex(h.ParentHash)
			rec.Timestamp = h.Timestamp
			rec.SlotStart = schema.Nanos(int64(h.Timestamp) * int64(time.Second))
			rec.Deadline = rec.SlotStart + schema.Nanos(r.slotDur)
			rec.TxCount = len(h.TxHashes)
			rec.LogsBloomEmpty = h.LogsBloom == [256]byte{}
			r.headers[n] = h
		}
		for _, p := range r.probes {
			rec.Probes[p] = &schema.ProbeResult{Status: schema.StatusLateArmed}
		}
		r.event(schema.Event{Type: schema.EventLateArmed, BlockNumber: &num, BlockHash: rec.BlockHash, Source: "head_watch",
			Reason: "node was already past this block when it would have been armed"})
		ctxT, cancel := context.WithCancel(context.Background())
		cancel()
		t := &target{number: n, ctx: ctxT, cancel: cancel, rec: rec, lateArmed: true, header: h}
		r.targets[n] = t
		if n > r.lastConfirmed {
			r.lastConfirmed = n
			r.lastProgress = time.Now()
		}
		r.maybeFinishLocked(t)
		r.mu.Unlock()
	}
	r.mu.Lock()
	if prev, ok := r.headers[r.lastConfirmed]; ok {
		hash := prev.Hash
		r.armLocked(r.lastConfirmed+1, true, &hash)
	} else {
		r.armLocked(r.lastConfirmed+1, true, nil)
	}
	r.armLocked(r.lastConfirmed+2, false, nil)
	r.mu.Unlock()
}

func (r *Runner) event(e schema.Event) {
	e.Envelope = r.env
	if e.At.IsZero() {
		e.At = r.clock.Now()
	}
	r.events.Write(e)
}

// readClock refreshes the manifest's clock quality. In budget mode the
// user's figure is authoritative and nothing is probed.
func (r *Runner) readClock(ctx context.Context, initial bool) {
	cq := &r.manifest.Clock
	cq.Mode = r.cfg.Clock.Mode
	if r.cfg.Clock.Mode == ClockModeBudget {
		cq.Source, cq.ErrorMs, cq.MaxErrorMs = "user_budget", r.cfg.Clock.ErrorBudgetMs, r.cfg.Clock.ErrorBudgetMs
		return
	}
	rd := hostinfo.ReadClock(ctx)
	errMs := rd.ErrorMs
	source := rd.Source
	if !rd.OK || errMs == 0 {
		errMs = r.cfg.Clock.ErrorBudgetMs
		source = rd.Source + "+budget_fallback"
	}
	if initial {
		cq.Source, cq.ErrorMs, cq.Synced, cq.Detail = source, errMs, rd.Synced, rd.Detail
		if !rd.OK {
			r.log.Warnf("clock quality unavailable from %s; using error budget %.1f ms", rd.Source, r.cfg.Clock.ErrorBudgetMs)
		}
	}
	if errMs > cq.MaxErrorMs {
		cq.MaxErrorMs = errMs
	}
	if !initial {
		r.event(schema.Event{Type: schema.EventClockSample, Source: rd.Source, Data: map[string]any{
			"error_ms": errMs, "synced": rd.Synced, "detail": rd.Detail}})
	}
}

func (r *Runner) sampleHost(ctx context.Context) {
	res := hostinfo.NewResourceSampler()
	steps := hostinfo.NewStepDetector(time.Duration(r.cfg.Clock.StepThresholdMs * float64(time.Millisecond)))
	stepTicker := time.NewTicker(time.Second)
	defer stepTicker.Stop()
	sampleTicker := time.NewTicker(time.Duration(r.cfg.SampleIntervalSeconds) * time.Second)
	defer sampleTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stepTicker.C:
			if drift, stepped := steps.Sample(); stepped {
				r.mu.Lock()
				r.manifest.Clock.Steps++
				var affected []uint64
				for n, t := range r.targets {
					t.rec.ClockStep = true
					affected = append(affected, n)
				}
				r.mu.Unlock()
				sort.Slice(affected, func(i, j int) bool { return affected[i] < affected[j] })
				r.event(schema.Event{Type: schema.EventClockStep, Source: "wall_vs_monotonic", Data: map[string]any{
					"drift_us": drift.Microseconds(), "affected_blocks": affected}})
				r.log.Warnf("wall clock stepped by %s; active targets flagged", drift)
			}
		case <-sampleTicker.C:
			r.event(schema.Event{Type: schema.EventResource, Source: "probe_process", Data: res.Sample()})
			r.readClock(ctx, false)
		}
	}
}

func (r *Runner) followBeacon(ctx context.Context) {
	r.beacon.stream(ctx, func(ev sseEvent) {
		e := schema.Event{Type: schema.EventCL, At: r.clock.At(ev.received), Source: "beacon_sse:" + ev.topic,
			Boundary: "beacon API event received by probe (post-import/validation milestone, not network arrival)", Data: ev.data}
		if s, ok := ev.data["slot"].(string); ok {
			if v, err := anyUint(s); err == nil {
				e.Slot = &v
			}
		}
		r.event(e)
	}, func(err error) {
		reason := ""
		if err != nil {
			reason = err.Error()
		}
		r.event(schema.Event{Type: schema.EventCLDisconnect, Source: "beacon_sse", Reason: reason})
	})
}
