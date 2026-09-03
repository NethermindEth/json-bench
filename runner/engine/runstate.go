package engine

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// Sink receives cumulative metric snapshots while the run is in progress.
type Sink interface {
	Push(ctx context.Context, snap Snapshot) error
	Close(ctx context.Context) error
}

// runState holds the live counters a snapshot is built from: the sample
// accumulator, the byte totals, and how many requests are in flight.
type runState struct {
	testName string
	accum    *Accumulator

	mu         sync.Mutex
	sentBytes  map[string]float64
	recvBytes  map[string]float64
	deliveries map[string]*Delivery
}

func newRunState(testName string, targets []*target) *runState {
	rs := &runState{
		testName:   testName,
		accum:      NewAccumulator(),
		sentBytes:  make(map[string]float64, len(targets)),
		recvBytes:  make(map[string]float64, len(targets)),
		deliveries: make(map[string]*Delivery, len(targets)),
	}
	for _, tgt := range targets {
		rs.sentBytes[tgt.name] = 0
		rs.recvBytes[tgt.name] = 0
		rs.deliveries[tgt.name] = &Delivery{}
	}
	return rs
}

func (rs *runState) begin(client string, at time.Time) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.delivery(client).Started = at
}

func (rs *runState) end(client string, at time.Time) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.delivery(client).Finished = at
}

func (rs *runState) scheduled(client string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.delivery(client).Scheduled++
}

func (rs *runState) dropped(client string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.delivery(client).Dropped++
}

func (rs *runState) sent(client string, queue, lateAfter time.Duration) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	d := rs.delivery(client)
	d.Sent++
	if lateAfter > 0 && queue > lateAfter {
		d.Late++
		d.QueueDelays = append(d.QueueDelays, queue)
	}
}

// delivery must be called with the lock held.
func (rs *runState) delivery(client string) *Delivery {
	d, ok := rs.deliveries[client]
	if !ok {
		d = &Delivery{}
		rs.deliveries[client] = d
	}
	return d
}

// Deliveries returns a copy of what each client has delivered so far.
func (rs *runState) Deliveries() map[string]Delivery {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	out := make(map[string]Delivery, len(rs.deliveries))
	for name, d := range rs.deliveries {
		copied := *d
		copied.QueueDelays = append([]time.Duration(nil), d.QueueDelays...)
		out[name] = copied
	}
	return out
}

func (rs *runState) observe(s Sample) {
	rs.accum.Add(s)

	rs.mu.Lock()
	rs.sentBytes[s.Client] += float64(s.RequestBytes)
	rs.recvBytes[s.Client] += float64(s.ResponseBytes)
	rs.mu.Unlock()
}

// enter and leave track concurrency so the in-flight gauge reflects what the
// generator was actually doing, rather than a configured maximum.
func (rs *runState) enter(client string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	d := rs.delivery(client)
	d.Inflight++
	if d.Inflight > d.InflightPeak {
		d.InflightPeak = d.Inflight
	}
}

func (rs *runState) leave(client string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if d := rs.delivery(client); d.Inflight > 0 {
		d.Inflight--
	}
}

func (rs *runState) byteTotals() (sent, recv map[string]float64) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	sent = make(map[string]float64, len(rs.sentBytes))
	recv = make(map[string]float64, len(rs.recvBytes))
	for name, v := range rs.sentBytes {
		sent[name] = v
	}
	for name, v := range rs.recvBytes {
		recv[name] = v
	}
	return sent, recv
}

// snapshot builds the cumulative state to publish.
func (rs *runState) snapshot(at time.Time) Snapshot {
	sent, recv := rs.byteTotals()
	return rs.accum.Snapshot(rs.testName, at, rs.Deliveries(), sent, recv)
}

// startPusher publishes a snapshot on the configured interval and returns a
// function that publishes a final one and closes the sink. Snapshots are
// cumulative from the run's start, matching what k6 emitted, so a dashboard
// panel reads "p99 so far" exactly as it always did.
//
// A sink failure is logged and the run continues: losing metric export is not a
// reason to lose the benchmark.
func (rs *runState) startPusher(ctx context.Context, opts Options, log *logrus.Logger) func() {
	if opts.Sink == nil {
		return func() {}
	}

	interval := opts.PushInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}

	// The pusher outlives ctx so the final snapshot still ships when a run is
	// cancelled; only the run loop stops early.
	pushCtx := context.WithoutCancel(ctx)

	done := make(chan struct{})
	stopped := make(chan struct{})

	go func() {
		defer close(stopped)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := opts.Sink.Push(pushCtx, rs.snapshot(time.Now())); err != nil {
					log.WithError(err).Warn("Failed to publish metrics; the run continues")
				}
			case <-done:
				return
			}
		}
	}()

	return func() {
		close(done)
		<-stopped

		if err := opts.Sink.Push(pushCtx, rs.snapshot(time.Now())); err != nil {
			log.WithError(err).Warn("Failed to publish the final metrics snapshot")
		}
		if err := opts.Sink.Close(pushCtx); err != nil {
			log.WithError(err).Warn("Failed to close the metrics sink")
		}
	}
}
