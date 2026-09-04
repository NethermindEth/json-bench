package engine

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrSaturated is returned when the in-flight limit was reached under the abort
// policy, meaning the generator could not offer the requested load.
var ErrSaturated = errors.New("generator could not sustain the requested rate")

// dispatchGrace is how far past the run's window a request may still be sent.
//
// Without it the last arrival of a ramped run — placed within a millisecond of
// the window's end — is dropped whenever the scheduler wakes a moment late,
// reporting a shortfall the generator did not have. A run that is genuinely
// behind is behind by far more than this, so the "stop rather than overrun"
// property is kept, and a hundred milliseconds of overrun on a run's length is
// immaterial.
const dispatchGrace = 100 * time.Millisecond

// Delivery is the load actually offered, as distinct from the load requested.
// Every field here is absent from the k6 pipeline, which is how a run that
// delivered a fraction of its target could read as a clean result.
type Delivery struct {
	Scheduled int
	Sent      int
	Late      int
	Dropped   int

	// QueueDelays holds the dispatch delay of every request that went out late,
	// so the shortfall has a distribution rather than only a count.
	QueueDelays []time.Duration

	Inflight     int
	InflightPeak int

	// WarmupSent counts requests issued before the measured window opened.
	// They are excluded from every field above, which all describe the measured
	// window only.
	WarmupSent int

	Started  time.Time
	Finished time.Time

	// LastDispatch is when the final request went out. The offered rate is
	// measured against this rather than against Finished, which also covers
	// draining the requests still in flight — seconds of it when the endpoint
	// is queueing, which would understate the rate the generator managed to
	// offer.
	LastDispatch time.Time
}

// Elapsed is how long the run actually took, which is what throughput divides by.
func (d Delivery) Elapsed() time.Duration {
	if d.Started.IsZero() {
		return 0
	}
	end := d.Finished
	if end.IsZero() {
		end = time.Now()
	}
	return end.Sub(d.Started)
}

// OfferedWindow is how long the generator spent dispatching, which is the
// period the offered rate is a rate over.
func (d Delivery) OfferedWindow() time.Duration {
	if d.Started.IsZero() || d.LastDispatch.IsZero() || !d.LastDispatch.After(d.Started) {
		return d.Elapsed()
	}
	return d.LastDispatch.Sub(d.Started)
}

// AchievedRate is the requests per second the generator actually offered.
func (d Delivery) AchievedRate() float64 {
	if window := d.OfferedWindow().Seconds(); window > 0 {
		return float64(d.Sent) / window
	}
	return 0
}

// MaxQueueDelay is the worst dispatch delay observed.
func (d Delivery) MaxQueueDelay() time.Duration {
	var out time.Duration
	for _, v := range d.QueueDelays {
		if v > out {
			out = v
		}
	}
	return out
}

// runClient dispatches the sequence against one target, recording what it
// delivered into the shared run state so a snapshot taken mid-run sees the
// shortfall as it develops rather than only at the end.
func runClient(
	ctx context.Context,
	tgt *target,
	batches []Batch,
	sched schedule,
	concurrency int,
	policy Saturation,
	start time.Time,
	rs *runState,
	onSample func(Sample),
) error {
	if concurrency < 1 {
		concurrency = 1
	}

	var (
		slots   = make(chan struct{}, concurrency)
		wg      sync.WaitGroup
		aborted bool
	)

	rs.begin(tgt.name, sched.measureFrom(start))
	deadline := start.Add(sched.duration + dispatchGrace)
	lateAfter := sched.lateThreshold()

	for i := 0; i < sched.total; i++ {
		due := sched.due(start, i)
		if sched.paced() && !sleepUntil(ctx, due) {
			break
		}

		warmup := sched.isWarmup(start, due)

		// Requests still unsent when the run's window closes are dropped rather
		// than extending the run, so the measured window matches the configured
		// one and a shortfall shows up as drops instead of a longer duration.
		if time.Now().After(deadline) {
			if !warmup {
				rs.scheduled(tgt.name)
				rs.dropped(tgt.name)
			}
			continue
		}

		if !warmup {
			rs.scheduled(tgt.name)
		}

		acquired, fatal := acquireSlot(ctx, slots, policy)
		if fatal {
			aborted = true
			break
		}
		if !acquired {
			if !warmup {
				rs.dropped(tgt.name)
			}
			continue
		}

		batch := batches[i]
		dispatched := time.Now()
		if !warmup {
			rs.dispatched(tgt.name, dispatched)
		}
		queue := time.Duration(0)
		if sched.paced() && dispatched.After(due) {
			queue = dispatched.Sub(due)
		}

		wg.Add(1)
		rs.enter(tgt.name)
		go func() {
			defer wg.Done()
			defer func() {
				rs.leave(tgt.name)
				<-slots
			}()

			samples := issue(ctx, tgt, batch, due, dispatched, queue)
			rs.sent(tgt.name, queue, lateAfter, warmup)
			for _, sample := range samples {
				sample.Warmup = warmup
				onSample(sample)
			}
		}()
	}

	wg.Wait()
	rs.end(tgt.name, time.Now())

	if aborted {
		return ErrSaturated
	}
	return ctx.Err()
}

// acquireSlot takes an in-flight slot under the configured policy. It reports
// whether a slot was taken, and whether the run should abort.
func acquireSlot(ctx context.Context, slots chan struct{}, policy Saturation) (acquired, fatal bool) {
	switch policy {
	case SaturationQueue:
		select {
		case slots <- struct{}{}:
			return true, false
		case <-ctx.Done():
			return false, false
		}
	default:
		select {
		case slots <- struct{}{}:
			return true, false
		default:
			return false, policy == SaturationAbort
		}
	}
}

// issue sends one round trip and returns a sample per JSON-RPC call it carried.
//
// Every member shares the round trip's timings, because every member's caller
// waited the whole round trip for its answer. The request bytes are shared too,
// so they are attributed to the first member rather than counted once per call.
func issue(ctx context.Context, tgt *target, batch Batch, due, dispatched time.Time, queue time.Duration) []Sample {
	res := tgt.do(ctx, batch.Payload)
	outcomes := classifyBatch(batch, res.status, res.body, res.err)

	samples := make([]Sample, 0, batch.Size())
	for i, req := range batch.Requests {
		sample := Sample{
			Client:           tgt.name,
			ClientType:       tgt.clientType,
			Name:             req.Name,
			Method:           req.Method,
			RequestID:        req.ID,
			Scheduled:        due,
			Start:            dispatched,
			Queue:            queue,
			Phases:           res.phases,
			Status:           res.status,
			Outcome:          outcomes[i].outcome,
			RPCCode:          outcomes[i].rpcCode,
			ResponseBytes:    outcomes[i].respSize,
			ConnectionReused: res.reused,
			BatchSize:        batch.Size(),
			BatchLeader:      i == 0,
		}
		if i == 0 {
			sample.RequestBytes = res.sentBytes
		}
		if res.err != nil {
			sample.Error = res.err.Error()
		}
		if sample.Name == "" {
			sample.Name = req.Method
		}
		samples = append(samples, sample)
	}
	return samples
}

// sleepUntil waits for the request's scheduled moment, reporting false if the
// run was cancelled first.
func sleepUntil(ctx context.Context, at time.Time) bool {
	wait := time.Until(at)
	if wait <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
