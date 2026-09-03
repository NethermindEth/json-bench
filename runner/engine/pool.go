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

	Started  time.Time
	Finished time.Time
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

// AchievedRate is the requests per second the generator actually offered.
func (d Delivery) AchievedRate() float64 {
	if elapsed := d.Elapsed().Seconds(); elapsed > 0 {
		return float64(d.Sent) / elapsed
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
	requests []Request,
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

	rs.begin(tgt.name, start)
	deadline := start.Add(sched.duration)
	lateAfter := sched.lateThreshold()

	for i := 0; i < sched.total; i++ {
		due := sched.due(start, i)
		if sched.paced() && !sleepUntil(ctx, due) {
			break
		}

		// Requests still unsent when the run's window closes are dropped rather
		// than extending the run, so the measured window matches the configured
		// one and a shortfall shows up as drops instead of a longer duration.
		if time.Now().After(deadline) {
			rs.scheduled(tgt.name)
			rs.dropped(tgt.name)
			continue
		}

		rs.scheduled(tgt.name)

		acquired, fatal := acquireSlot(ctx, slots, policy)
		if fatal {
			aborted = true
			break
		}
		if !acquired {
			rs.dropped(tgt.name)
			continue
		}

		req := requests[i]
		dispatched := time.Now()
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

			sample := issue(ctx, tgt, req, due, dispatched, queue)
			rs.sent(tgt.name, queue, lateAfter)
			onSample(sample)
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

func issue(ctx context.Context, tgt *target, req Request, due, dispatched time.Time, queue time.Duration) Sample {
	res := tgt.do(ctx, req.Payload)
	outcome, rpcCode := classify(res.status, res.body, res.err)

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
		Outcome:          outcome,
		RPCCode:          rpcCode,
		RequestBytes:     res.sentBytes,
		ResponseBytes:    len(res.body),
		ConnectionReused: res.reused,
	}
	if res.err != nil {
		sample.Error = res.err.Error()
	}
	if sample.Name == "" {
		sample.Name = req.Method
	}
	return sample
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
