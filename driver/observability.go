package driver

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

var ErrRxOverflow = errors.New("CAN receive queue overflow")

var closedDriverErrors = func() <-chan error {
	ch := make(chan error)
	close(ch)
	return ch
}()

// DriverStats is a snapshot of receive-side queue pressure.
type DriverStats struct {
	SourceDropped     uint64
	SubscriberDropped uint64
}

// ReceiveOverflowError identifies where a CAN frame was dropped.
type ReceiveOverflowError struct {
	Stage   string
	Dropped uint64
}

func (e *ReceiveOverflowError) Error() string {
	return fmt.Sprintf("%s: stage=%s dropped=%d", ErrRxOverflow, e.Stage, e.Dropped)
}

func (e *ReceiveOverflowError) Unwrap() error {
	return ErrRxOverflow
}

// ObservableCANDriver is an optional extension implemented by the built-in
// drivers. Existing third-party CANDriver implementations remain compatible.
type ObservableCANDriver interface {
	Errors() <-chan error
	Stats() DriverStats
}

// RxSubscriber is an optional extension that lets callers release a receive
// subscription before the driver itself is stopped.
type RxSubscriber interface {
	SubscribeRx(buffer int) (<-chan CanFrame, func())
}

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
	t.report(&ReceiveOverflowError{Stage: "driver-source", Dropped: count})
}

func (t *driverTelemetry) reportSubscriberDrop() {
	count := t.subscriberDropped.Add(1)
	t.report(&ReceiveOverflowError{Stage: "subscriber", Dropped: count})
}

func (t *driverTelemetry) stats() DriverStats {
	if t == nil {
		return DriverStats{}
	}
	return DriverStats{
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
	mu        sync.RWMutex
	telemetry *driverTelemetry
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

func (o *driverObservability) Errors() <-chan error {
	return o.currentTelemetry().errors
}

func (o *driverObservability) Stats() DriverStats {
	return o.currentTelemetry().stats()
}

func (o *driverObservability) closeTelemetry() {
	o.mu.RLock()
	current := o.telemetry
	o.mu.RUnlock()
	current.close()
}

func (o *driverObservability) publishRx(ctx context.Context, destination chan<- CanFrame, frame CanFrame) bool {
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
