package backend

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

var closedDriverErrors = func() <-chan error {
	ch := make(chan error)
	close(ch)
	return ch
}()

type driverTelemetry struct {
	mu                sync.Mutex
	errors            chan error
	closed            bool
	closeOnce         sync.Once
	sourceDropped     atomic.Uint64
	subscriberDropped atomic.Uint64
}

func newDriverTelemetry() *driverTelemetry {
	return &driverTelemetry{errors: make(chan error, 16)}
}

func (t *driverTelemetry) report(err error) {
	if t == nil || err == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	select {
	case t.errors <- err:
	default:
	}
}

func (t *driverTelemetry) reportSourceDrop() {
	count := t.sourceDropped.Add(1)
	t.report(&receiveOverflowError{Stage: "driver-source", Dropped: count})
}

func (t *driverTelemetry) reportSubscriberDrop() {
	count := t.subscriberDropped.Add(1)
	t.report(&receiveOverflowError{Stage: "subscriber", Dropped: count})
}

func (t *driverTelemetry) stats() driverStats {
	if t == nil {
		return driverStats{}
	}
	return driverStats{
		SourceDropped:     t.sourceDropped.Load(),
		SubscriberDropped: t.subscriberDropped.Load(),
	}
}

func (t *driverTelemetry) close() {
	if t == nil {
		return
	}
	t.closeOnce.Do(func() {
		t.mu.Lock()
		t.closed = true
		close(t.errors)
		t.mu.Unlock()
	})
}

// driverObservability centralizes telemetry shared by all hardware backends.
// Its zero value is ready for use.
type driverObservability struct {
	mu               sync.RWMutex
	telemetry        *driverTelemetry
	loadMeter        busLoadMeter
	relativeLogClock relativeLogClock
}

func (o *driverObservability) resetTelemetry() *driverTelemetry {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.telemetry != nil {
		o.telemetry.mu.Lock()
		closed := o.telemetry.closed
		o.telemetry.mu.Unlock()
		if !closed {
			return o.telemetry
		}
	}
	o.telemetry = newDriverTelemetry()
	return o.telemetry
}

func (o *driverObservability) resetTelemetryWith(cfg config) *driverTelemetry {
	t := o.resetTelemetry()
	o.loadMeter.configure(cfg)
	o.relativeLogClock.reset()
	return t
}

func (o *driverObservability) relativeLogTimes(timestampUS, wrapPeriodUS uint64) (elapsedUS, deltaUS uint64) {
	return o.relativeLogClock.observe(timestampUS, wrapPeriodUS)
}

func (o *driverObservability) currentTelemetry() *driverTelemetry {
	o.mu.RLock()
	current := o.telemetry
	o.mu.RUnlock()
	if current != nil {
		return current
	}

	o.mu.Lock()
	defer o.mu.Unlock()
	if o.telemetry == nil {
		o.telemetry = newDriverTelemetry()
	}
	return o.telemetry
}

func (o *driverObservability) errors() <-chan error {
	return o.currentTelemetry().errors
}

func (o *driverObservability) stats() driverStats {
	return o.currentTelemetry().stats()
}

func (o *driverObservability) busLoad() busLoadSnapshot {
	return o.loadMeter.snapshot(time.Now())
}

func (o *driverObservability) recordBusTx(id int32, fd, brs bool, data []byte) {
	o.loadMeter.recordTx(id, fd, brs, data, time.Now())
}

func (o *driverObservability) observeBusFrame(frame frame) {
	o.observeBusFrameWithWrap(frame, 0)
}

func (o *driverObservability) observeBusFrameWithWrap(frame frame, wrapPeriodUS uint64) {
	o.loadMeter.observeWithWrap(frame, time.Now(), wrapPeriodUS)
}

func (o *driverObservability) observeBusFrameWithBitCount(frame frame, totalBitCount uint16) {
	o.loadMeter.observeWithBitCount(frame, totalBitCount, time.Now(), 0)
}

func (o *driverObservability) closeTelemetry() {
	o.mu.RLock()
	current := o.telemetry
	o.mu.RUnlock()
	current.close()
}

func (o *driverObservability) publishRx(ctx context.Context, destination chan<- frame, frame frame) bool {
	return o.publishRxWithWrap(ctx, destination, frame, 0)
}

func (o *driverObservability) publishRxWithWrap(ctx context.Context, destination chan<- frame, frame frame, wrapPeriodUS uint64) bool {
	o.observeBusFrameWithWrap(frame, wrapPeriodUS)
	return o.publishObservedRx(ctx, destination, frame)
}

func (o *driverObservability) publishRxWithBitCount(ctx context.Context, destination chan<- frame, frame frame, totalBitCount uint16) bool {
	o.observeBusFrameWithBitCount(frame, totalBitCount)
	return o.publishObservedRx(ctx, destination, frame)
}

func (o *driverObservability) publishObservedRx(ctx context.Context, destination chan<- frame, frame frame) bool {
	select {
	case <-ctx.Done():
		return false
	case destination <- frame:
		return true
	default:
		o.currentTelemetry().reportSourceDrop()
		return false
	}
}
