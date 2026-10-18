package backend

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LoveWonYoung/canbuskit/v2/can"
)

var closedErrors = func() <-chan error {
	ch := make(chan error)
	close(ch)
	return ch
}()

type telemetry struct {
	mu                sync.Mutex
	errors            chan error
	sourceDropped     atomic.Uint64
	subscriberDropped atomic.Uint64
}

func newTelemetry() *telemetry {
	return &telemetry{errors: make(chan error, 16)}
}

func (t *telemetry) report(err error) {
	if t == nil || err == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	select {
	case t.errors <- err:
	default:
	}
}

func (t *telemetry) reportSourceDrop() {
	count := t.sourceDropped.Add(1)
	t.report(&can.ReceiveOverflowError{Stage: "source", Dropped: count})
}

func (t *telemetry) reportSubscriberDrop() {
	count := t.subscriberDropped.Add(1)
	t.report(&can.ReceiveOverflowError{Stage: "subscriber", Dropped: count})
}

func (t *telemetry) stats() can.Stats {
	if t == nil {
		return can.Stats{}
	}
	return can.Stats{
		SourceDropped:     t.sourceDropped.Load(),
		SubscriberDropped: t.subscriberDropped.Load(),
	}
}

func (t *telemetry) resetCounts() {
	if t == nil {
		return
	}
	t.sourceDropped.Store(0)
	t.subscriberDropped.Store(0)
}

// observability centralizes telemetry shared by all hardware backends.
// Its zero value is ready for use. The error channel is created on first use
// and stays open for the life of the owning driver.
type observability struct {
	mu        sync.Mutex
	events    *telemetry
	loadMeter busLoadMeter
	clock     relativeLogClock
}

func (o *observability) telemetry() *telemetry {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.events == nil {
		o.events = newTelemetry()
	}
	return o.events
}

// prepare resets session counters and bus-load state for a new Open.
// It returns the same error telemetry for the life of the driver.
func (o *observability) prepare(cfg can.Config) *telemetry {
	events := o.telemetry()
	events.resetCounts()
	o.loadMeter.configure(cfg)
	o.clock.reset()
	return events
}

func (o *observability) relativeLogTimes(timestampUS, wrapPeriodUS uint64) (elapsedUS, deltaUS uint64) {
	return o.clock.observe(timestampUS, wrapPeriodUS)
}

func (o *observability) errors() <-chan error {
	return o.telemetry().errors
}

func (o *observability) stats() can.Stats {
	return o.telemetry().stats()
}

func (o *observability) busLoad() can.BusLoad {
	return o.loadMeter.snapshot(time.Now())
}

func (o *observability) recordBusTx(frame can.Frame) {
	frame.Direction = can.Transmit
	o.loadMeter.recordTx(frame, time.Now())
}

func (o *observability) observeBusFrame(frame can.Frame) {
	o.observeBusFrameWithWrap(frame, 0)
}

func (o *observability) observeBusFrameWithWrap(frame can.Frame, wrapPeriodUS uint64) {
	o.loadMeter.observeWithWrap(frame, time.Now(), wrapPeriodUS)
}

func (o *observability) observeBusFrameWithBitCount(frame can.Frame, totalBitCount uint16) {
	o.loadMeter.observeWithBitCount(frame, totalBitCount, time.Now(), 0)
}

func (o *observability) publishRx(ctx context.Context, destination chan<- can.Frame, frame can.Frame) bool {
	return o.publishRxWithWrap(ctx, destination, frame, 0)
}

func (o *observability) publishRxWithWrap(ctx context.Context, destination chan<- can.Frame, frame can.Frame, wrapPeriodUS uint64) bool {
	o.observeBusFrameWithWrap(frame, wrapPeriodUS)
	return o.publishObservedRx(ctx, destination, frame)
}

func (o *observability) publishRxWithBitCount(ctx context.Context, destination chan<- can.Frame, frame can.Frame, totalBitCount uint16) bool {
	o.observeBusFrameWithBitCount(frame, totalBitCount)
	return o.publishObservedRx(ctx, destination, frame)
}

func (o *observability) publishObservedRx(ctx context.Context, destination chan<- can.Frame, frame can.Frame) bool {
	select {
	case <-ctx.Done():
		return false
	case destination <- frame:
		return true
	default:
		o.telemetry().reportSourceDrop()
		return false
	}
}
