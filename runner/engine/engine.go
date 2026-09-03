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
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/engine/promrw"
	"github.com/jsonrpc-bench/runner/types"
)

// SampleFilename is where a run's per-request records are written.
const SampleFilename = "samples.jsonl.gz"

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

	// WriteSamples persists the per-request records. On by default because
	// offline re-aggregation and any later comparison depend on them.
	WriteSamples bool

	Logger *logrus.Logger
}

func DefaultOptions() Options {
	return Options{
		Saturation:   SaturationQueue,
		Transport:    DefaultTransportOptions(),
		WriteSamples: true,
		PushInterval: promrw.DefaultPushInterval,
		Logger:       logrus.New(),
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

	sched, err := newSchedule(cfg, len(requests))
	if err != nil {
		return nil, nil, err
	}
	if sched.total <= 0 {
		return nil, nil, fmt.Errorf("the load configuration schedules no requests")
	}
	if sched.total < len(requests) {
		requests = requests[:sched.total]
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

	log.WithFields(logrus.Fields{
		"clients":     len(targets),
		"requests":    sched.total,
		"concurrency": concurrency,
		"saturation":  opts.Saturation,
	}).Info("Running benchmark")

	start := time.Now()
	run := newRunState(cfg.TestName, targets)
	stopPusher := run.startPusher(ctx, opts, log)
	runErr := dispatch(ctx, targets, requests, sched, concurrency, opts, run, writer)
	stopPusher()
	end := time.Now()
	deliveries := run.Deliveries()

	clients := make(map[string]*types.ClientMetrics, len(targets))
	for _, tgt := range targets {
		clients[tgt.name] = run.accum.ClientMetrics(tgt.name, deliveries[tgt.name])
	}

	for _, tgt := range targets {
		reportDelivery(log, tgt.name, cfg, sched, deliveries[tgt.name])
	}

	result := &types.BenchmarkResult{
		Summary:       runSummary(cfg, sched, deliveries, opts),
		ClientMetrics: clients,
		Timestamp:     time.Now().Format(time.DateTime),
		StartTime:     start.Format(time.DateTime),
		EndTime:       end.Format(time.DateTime),
		Duration:      end.Sub(start).String(),
		ResponsesDir:  opts.OutputDir,
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
	requests []Request,
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
			err := runClient(ctx, tgt, requests, sched, concurrency, opts.Saturation, start, run,
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

func runSummary(cfg *config.Config, sched schedule, deliveries map[string]Delivery, opts Options) map[string]any {
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
			"duration":    cfg.Duration,
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

// Version identifies the engine in run provenance and in the remote-write
// User-Agent, so a stored run can be traced to what produced it.
const Version = "1"
