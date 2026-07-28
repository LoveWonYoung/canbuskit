package driver

import (
	"context"
	"errors"
	"testing"
)

func TestPublishRxReportsSourceOverflow(t *testing.T) {
	var observable driverObservability
	beforeInit := observable.Errors()
	observable.resetTelemetry()
	if afterInit := observable.Errors(); beforeInit != afterInit {
		t.Fatal("resetTelemetry replaced an active pre-init error subscription")
	}
	destination := make(chan CanFrame)

	if observable.publishRx(context.Background(), destination, CanFrame{}) {
		t.Fatal("publishRx should report a full unbuffered destination")
	}
	if got := observable.Stats().SourceDropped; got != 1 {
		t.Fatalf("SourceDropped = %d, want 1", got)
	}
	select {
	case err := <-observable.Errors():
		if !errors.Is(err, ErrRxOverflow) {
			t.Fatalf("error = %v, want ErrRxOverflow", err)
		}
	default:
		t.Fatal("expected an overflow error")
	}
}

func TestRxFanoutReportsSubscriberOverflowAndUnsubscribes(t *testing.T) {
	telemetry := newDriverTelemetry()
	fanout := &rxFanout{
		subs:      make(map[chan CanFrame]struct{}),
		telemetry: telemetry,
	}
	received, unsubscribe := fanout.Subscribe(1)

	fanout.dispatch(CanFrame{ID: 1})
	fanout.dispatch(CanFrame{ID: 2})

	if got := telemetry.stats().SubscriberDropped; got != 1 {
		t.Fatalf("SubscriberDropped = %d, want 1", got)
	}
	first := <-received
	if first.ID != 1 {
		t.Fatalf("received ID = %d, want 1", first.ID)
	}

	unsubscribe()
	if _, ok := <-received; ok {
		t.Fatal("subscription channel should be closed")
	}
	unsubscribe()
}
