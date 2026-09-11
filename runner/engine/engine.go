// Package engine runs a JSON-RPC load test in process: it schedules arrivals,
// dispatches them against one or more clients, classifies every response and
// keeps each request as a sample.
//
// Keeping samples is the point. Aggregates alone cannot say whether a run
// delivered the load it requested, cannot distinguish a JSON-RPC error from a
// successful call, and cannot yield a percentile for a client as a whole — only
// the mean of its methods' percentiles, which is not a percentile of anything.
package engine

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/engine/promrw"
	"github.com/jsonrpc-bench/runner/types"
)

// SampleFilename is where a run's per-request records are written.
const (
	SampleFilename   = "samples.jsonl.gz"
	ManifestFilename = "manifest.json"
)

// Options are the run's knobs that are not part of the benchmark config.
type Options struct {
	OutputDir  string
	Saturation Saturation
	Transport  TransportOptions

	// Sink receives cumulative metric snapshots during the run. Nil disables
	// metric export; a failing sink warns and never fails the benchmark.
	Sink Sink

	// PushInterval is how often Sink is given a snapshot.
	PushInterval time.Duration

	// TargetMetrics controls reading each node's own metrics endpoint during
	// the run. Scraping happens only for clients that declare a metrics_url.
	TargetMetrics TargetMetricsOptions

	// WriteSamples persists the per-request records. On by default because
	// offline re-aggregation and any later comparison depend on them.
	WriteSamples bool

	// SkipPreflight runs without identifying the targets first. The manifest
	// records that it was skipped, since the provenance is then only what the
	// config claimed rather than what answered.
	SkipPreflight bool
	Preflight     PreflightOptions

	Logger *logrus.Logger
}

func DefaultOptions() Options {
	return Options{
		Saturation:    SaturationQueue,
		Transport:     DefaultTransportOptions(),
		WriteSamples:  true,
		PushInterval:  promrw.DefaultPushInterval,
		TargetMetrics: DefaultTargetMetricsOptions(),
		Logger:        logrus.New(),
	}
}

// Run executes the benchmark and returns the report the exporters and historic
// storage consume.
func Run(ctx context.Context, cfg *config.Config, opts Options) (*types.BenchmarkResult, []ThresholdBreach, error) {
	if opts.Logger == nil {
		opts.Logger = logrus.New()
	}
	if opts.Saturation == "" {
		opts.Saturation = SaturationQueue
	}
	log := opts.Logger

	requests, err := Sequence(cfg)
	if err != nil {
		return nil, nil, err
	}
	if len(requests) == 0 {
		return nil, nil, fmt.Errorf("the request sequence is empty")
	}

	batchSize := cfg.BatchSize
	if batchSize < 1 {
		batchSize = 1
	}
	batches, err := BuildBatches(requests, batchSize)
	if err != nil {
		return nil, nil, err
	}

	sched, err := newSchedule(cfg, len(batches), batchSize)
	if err != nil {
		return nil, nil, err
	}
	if sched.total <= 0 {
		return nil, nil, fmt.Errorf("the load configuration schedules no requests")
	}
	if sched.total < len(batches) {
		batches = batches[:sched.total]
	}

	concurrency := cfg.VUs
	if concurrency < 1 {
		concurrency = 1
	}

	targets := make([]*target, 0, len(cfg.ResolvedClients))
	for _, client := range cfg.ResolvedClients {
		tgt, err := newTarget(client, concurrency, opts.Transport)
		if err != nil {
			return nil, nil, err
		}
		targets = append(targets, tgt)
	}

	provenance, preflightErr := runPreflight(ctx, targets, opts, log)
	if preflightErr != nil {
		return nil, nil, preflightErr
	}

	var writer *SampleWriter
	if opts.WriteSamples && opts.OutputDir != "" {
		writer, err = NewSampleWriter(filepath.Join(opts.OutputDir, SampleFilename))
		if err != nil {
			return nil, nil, err
		}
		defer func() {
			if err := writer.Close(); err != nil {
				log.WithError(err).Warn("Failed to close the sample file")
			}
		}()
	}

	fields := logrus.Fields{
		"clients":     len(targets),
		"requests":    sched.total,
		"concurrency": concurrency,
		"saturation":  opts.Saturation,
	}
	if sched.warmup > 0 {
		fields["warmup"] = sched.warmup.String()
	}
	if len(cfg.Stages) > 0 {
		fields["stages"] = len(cfg.Stages)
	}
	if batchSize > 1 {
		fields["batch_size"] = batchSize
		fields["requests"] = sched.total * batchSize
		fields["batches"] = sched.total
	}
	log.WithFields(fields).Info("Running benchmark")
	warnRetention(log, sched.total*batchSize*len(targets))

	start := time.Now()
	run := newRunState(cfg.TestName, targets)
	stopScrapers := run.startTargetScrapers(ctx, cfg, opts, log)
	stopPusher := run.startPusher(ctx, opts, log)
	runErr := dispatch(ctx, targets, batches, sched, concurrency, opts, run, writer)
	// The scrapers stop before the final push, so its snapshot carries the
	// node-side figures for the whole run.
	stopScrapers()
	stopPusher()
	end := time.Now()
	deliveries := run.Deliveries()

	clients := make(map[string]*types.ClientMetrics, len(targets))
	for _, tgt := range targets {
		cm := run.accum.ClientMetrics(tgt.name, deliveries[tgt.name])
		cm.Delivery.TargetRPS = float64(cfg.RPS)
		cm.TargetMetrics = run.targetMetrics(tgt.name)
		clients[tgt.name] = cm
	}

	for _, tgt := range targets {
		reportDelivery(log, tgt.name, cfg, sched, deliveries[tgt.name])
	}

	result := &types.BenchmarkResult{
		Summary:       runSummary(cfg, sched, deliveries, opts, batchSize),
		ClientMetrics: clients,
		Timestamp:     time.Now().Format(time.DateTime),
		StartTime:     start.Format(time.DateTime),
		EndTime:       end.Format(time.DateTime),
		Duration:      end.Sub(start).String(),
		ResponsesDir:  opts.OutputDir,
		Manifest:      buildManifest(cfg, opts, provenance, start, end),
	}

	if comparisons := run.accum.CompareClients(); len(comparisons) > 0 {
		result.Comparison = &types.ComparisonResult{Methods: comparisons}
	}

	thresholds, err := collectConfigThresholds(cfg)
	if err != nil {
		return result, nil, err
	}
	breaches, err := EvaluateThresholds(thresholds, clients)
	if err != nil {
		return result, nil, err
	}

	return result, breaches, runErr
}

func dispatch(
	ctx context.Context,
	targets []*target,
	batches []Batch,
	sched schedule,
	concurrency int,
	opts Options,
	run *runState,
	writer *SampleWriter,
) error {
	var (
		mu   sync.Mutex
		errs []error
		wg   sync.WaitGroup
	)

	// Every client starts from one instant, so the same request ordinal is due
	// at the same moment everywhere and the clients stay comparable.
	start := time.Now()

	for _, tgt := range targets {
		wg.Add(1)
		go func(tgt *target) {
			defer wg.Done()
			err := runClient(ctx, tgt, batches, sched, concurrency, opts.Saturation, start, run,
				func(s Sample) {
					run.observe(s)
					mu.Lock()
					if writer != nil {
						if writeErr := writer.Write(s); writeErr != nil {
							errs = append(errs, writeErr)
						}
					}
					mu.Unlock()
				})

			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("client %s: %w", tgt.name, err))
				mu.Unlock()
			}
		}(tgt)
	}

	wg.Wait()

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// reportDelivery states what load was actually offered. A run that delivered a
// fraction of its target must not be readable as a clean result, so the
// shortfall is logged at warning level with the numbers that show it.
func reportDelivery(log *logrus.Logger, client string, cfg *config.Config, sched schedule, d Delivery) {
	fields := logrus.Fields{
		"client":       client,
		"scheduled":    d.Scheduled,
		"sent":         d.Sent,
		"late":         d.Late,
		"dropped":      d.Dropped,
		"achieved_rps": fmt.Sprintf("%.1f", d.AchievedRate()),
		"elapsed":      d.Elapsed().Round(time.Millisecond).String(),
	}
	if cfg.RPS > 0 {
		fields["target_rps"] = cfg.RPS
	}

	if d.WarmupSent > 0 {
		entry := log.WithField("client", client)
		entry.Infof("%s discarded %d warmup request(s); everything reported below describes the measured window only",
			client, d.WarmupSent)
	}

	entry := log.WithFields(fields)
	switch {
	case d.Dropped > 0:
		entry.Warnf("%s offered %d of %d scheduled requests: %d were dropped because the in-flight limit (vus=%d) was reached. The latency below describes only the requests that were sent",
			client, d.Sent, d.Scheduled, d.Dropped, cfg.VUs)
	case d.Late > 0:
		entry.Warnf("%s sent every request but %d went out more than one arrival interval late (worst %s); the endpoint was slower than the requested rate",
			client, d.Late, d.MaxQueueDelay().Round(time.Millisecond))
	default:
		entry.Infof("%s delivered the requested load", client)
	}
}

func runSummary(cfg *config.Config, sched schedule, deliveries map[string]Delivery, opts Options, batchSize int) map[string]any {
	clients := make(map[string]any, len(deliveries))
	for name, d := range deliveries {
		clients[name] = map[string]any{
			"scheduled":          d.Scheduled,
			"sent":               d.Sent,
			"late":               d.Late,
			"dropped":            d.Dropped,
			"achieved_rps":       d.AchievedRate(),
			"elapsed_seconds":    d.Elapsed().Seconds(),
			"max_queue_delay_ms": msOf(d.MaxQueueDelay()),
			"inflight_peak":      d.InflightPeak,
			"warmup_sent":        d.WarmupSent,
		}
	}

	return map[string]any{
		"engine": map[string]any{
			"name":               "native",
			"saturation":         string(opts.Saturation),
			"accept_compression": opts.Transport.AcceptCompression,
			"reuse_connections":  opts.Transport.ReuseConnections,
			"http2":              opts.Transport.HTTP2,
		},
		"load": map[string]any{
			"target_rps":  cfg.RPS,
			"iterations":  cfg.Iterations,
			"concurrency": cfg.VUs,
			"batch_size":  batchSize,
			"duration":    sched.duration.String(),
			"warmup":      sched.warmup.String(),
			"stages":      len(cfg.Stages),
			"seed":        cfg.Seed,
			"requests":    sched.total,
		},
		"delivery": clients,
	}
}

// collectConfigThresholds keys each call's thresholds on the identifier the
// per-method breakdown uses, so a threshold and the metrics it judges agree.
func collectConfigThresholds(cfg *config.Config) ([]Threshold, error) {
	var calls []*callThresholds
	for _, call := range cfg.Calls {
		if len(call.Thresholds) == 0 {
			continue
		}
		target := call.Method
		if target == "" {
			target = call.Name
		}
		calls = append(calls, &callThresholds{target: target, expressions: call.Thresholds})
	}
	return CollectThresholds(calls)
}

// retainedBytesPerSample is what one observation costs the accumulator, measured
// rather than derived, and pinned by a test. Exact percentiles are computed from
// every observation, so the cost is linear in the request count.
const retainedBytesPerSample = 93

// retentionWarnThreshold is where the generator's own footprint starts to matter
// on a run that shares a host with the node it measures.
const retentionWarnThreshold = 512 << 20

// warnRetention says up front what a long run will cost in memory. A load
// generator running on the node it measures competes with it, so the size is
// worth knowing before the run rather than from the OOM killer after it.
func warnRetention(log logrus.FieldLogger, requests int) {
	projected := int64(requests) * retainedBytesPerSample
	if projected < retentionWarnThreshold {
		return
	}
	log.Warnf("this run will retain about %d MB of samples in memory for exact percentiles; "+
		"on a run sharing a host with the target that competes with it", projected>>20)
}

// Version identifies the engine in run provenance and in the remote-write
// User-Agent, so a stored run can be traced to what produced it.
const Version = "1"

// ErrorRateSemanticsRPCAware names what the error rate counts. It is recorded
// on every run so a later comparison can refuse to read a semantics change as
// a regression: JSON-RPC errors arrive as HTTP 200 and an HTTP-only pipeline
// could not see them, so the same node measured both ways looks worse.
const ErrorRateSemanticsRPCAware = "http_and_jsonrpc_errors"

// IdentifyTargets probes the configured clients without running any load. The
// rate search uses it to identify the targets once, rather than once per probe.
func IdentifyTargets(ctx context.Context, cfg *config.Config, opts Options) ([]types.ClientProvenance, error) {
	if opts.Logger == nil {
		opts.Logger = logrus.New()
	}

	concurrency := cfg.VUs
	if concurrency < 1 {
		concurrency = 1
	}
	targets := make([]*target, 0, len(cfg.ResolvedClients))
	for _, client := range cfg.ResolvedClients {
		tgt, err := newTarget(client, concurrency, opts.Transport)
		if err != nil {
			return nil, err
		}
		targets = append(targets, tgt)
	}

	return runPreflight(ctx, targets, opts, opts.Logger)
}

// runPreflight identifies the targets and refuses the run when the answer makes
// it meaningless. An unhealthy target is a warning rather than a refusal: a
// syncing node is still a legitimate thing to measure as long as the report says
// that is what was measured.
func runPreflight(ctx context.Context, targets []*target, opts Options, log *logrus.Logger) ([]types.ClientProvenance, error) {
	if opts.SkipPreflight {
		log.Warn("Skipping preflight: the run will not record what the targets actually were")
		return nil, nil
	}

	provenance, err := Preflight(ctx, targets, opts.Preflight)
	if err != nil {
		return provenance, err
	}

	for _, info := range provenance {
		fields := logrus.Fields{
			"client":  info.Name,
			"version": info.ClientVersion,
			"chain":   info.ChainID,
			"head":    info.HeadBlock,
		}
		if len(info.ProbeErrors) > 0 {
			log.WithFields(fields).Warnf("%s did not answer every identity probe, so its provenance is incomplete: %s",
				info.Name, strings.Join(info.ProbeErrors, "; "))
		}
		if healthy, reason := TargetHealth(info); !healthy {
			log.WithFields(fields).Warnf("%s may not be fit to measure: %s", info.Name, reason)
			continue
		}
		log.WithFields(fields).Infof("%s is ready", info.Name)
	}
	return provenance, nil
}

func buildManifest(cfg *config.Config, opts Options, provenance []types.ClientProvenance, start, end time.Time) types.RunManifest {
	clients := provenance
	if len(clients) == 0 {
		clients = make([]types.ClientProvenance, 0, len(cfg.ResolvedClients))
		for _, client := range cfg.ResolvedClients {
			clients = append(clients, types.ClientProvenance{
				Name: client.Name,
				Type: client.Type,
				URL:  client.URL,
			})
		}
	}

	return types.RunManifest{
		Engine:             "native",
		EngineVersion:      Version,
		ErrorRateSemantics: ErrorRateSemanticsRPCAware,
		TestName:           cfg.TestName,
		Seed:               cfg.Seed,
		Saturation:         string(opts.Saturation),
		TargetRPS:          cfg.RPS,
		Iterations:         cfg.Iterations,
		Concurrency:        cfg.VUs,
		BatchSize:          cfg.BatchSize,
		Duration:           cfg.Duration,
		AcceptCompression:  opts.Transport.AcceptCompression,
		ReuseConnections:   opts.Transport.ReuseConnections,
		HTTP2:              opts.Transport.HTTP2,
		StartTime:          start.Format(time.RFC3339),
		EndTime:            end.Format(time.RFC3339),
		PreflightSkipped:   opts.SkipPreflight,
		Clients:            clients,
	}
}
