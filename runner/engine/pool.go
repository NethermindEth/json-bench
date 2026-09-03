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
	// so the shortfall has a distribution rather than just a count.
	QueueDelays []time.Duration

	Started  time.Time
	Finished time.Time
}

// Elapsed is how long the run actually took, which is what throughput divides by.
func (d Delivery) Elapsed() time.Duration {
	if d.Started.IsZero() || d.Finished.IsZero() {
		return 0
	}
	return d.Finished.Sub(d.Started)
}

// AchievedRate is the requests per second the generator actually offered.
func (d Delivery) AchievedRate() float64 {
	if elapsed := d.Elapsed().Seconds(); elapsed > 0 {
		return float64(d.Sent) / elapsed
	}
	return 0
}

// runClient dispatches the sequence against one target and returns its samples
// alongside what was actually delivered.
func runClient(
	ctx context.Context,
	tgt *target,
	requests []Request,
	sched schedule,
	concurrency int,
	policy Saturation,
	start time.Time,
	onSample func(Sample),
) (Delivery, error) {
	if concurrency < 1 {
		concurrency = 1
	}

	var (
		slots    = make(chan struct{}, concurrency)
		wg       sync.WaitGroup
		mu       sync.Mutex
		delivery = Delivery{Started: start}
		aborted  bool
	)

	deadline := start.Add(sched.duration)
	lateAfter := sched.lateThreshold()

	for i := 0; i < sched.total; i++ {
		due := sched.due(start, i)
		if sched.paced() {
			if !sleepUntil(ctx, due) {
				break
			}
		}

		// Requests still unsent when the run's window closes are dropped rather
		// than extending the run, so the measured window matches the configured
		// one and a shortfall shows up as drops instead of a longer duration.
		if time.Now().After(deadline) {
			mu.Lock()
			delivery.Scheduled++
			delivery.Dropped++
			mu.Unlock()
			continue
		}

		mu.Lock()
		delivery.Scheduled++
		mu.Unlock()

		acquired, fatal := acquireSlot(ctx, slots, policy)
		if fatal {
			mu.Lock()
			aborted = true
			mu.Unlock()
			break
		}
		if !acquired {
			mu.Lock()
			delivery.Dropped++
			mu.Unlock()
			continue
		}

		req := requests[i]
		dispatched := time.Now()
		queue := time.Duration(0)
		if sched.paced() && dispatched.After(due) {
			queue = dispatched.Sub(due)
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()

			sample := issue(ctx, tgt, req, due, dispatched, queue)

			mu.Lock()
			delivery.Sent++
			if lateAfter > 0 && queue > lateAfter {
				delivery.Late++
				delivery.QueueDelays = append(delivery.QueueDelays, queue)
			}
			mu.Unlock()

			onSample(sample)
		}()
	}

	wg.Wait()
	delivery.Finished = time.Now()

	if aborted {
		return delivery, ErrSaturated
	}
	return delivery, ctx.Err()
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
