package backend

import (
	"context"
	"errors"
	"testing"
)

func TestPublishRxReportsSourceOverflow(t *testing.T) {
	var observable driverObservability
	beforeInit := observable.errors()
	observable.resetTelemetry()
	if afterInit := observable.errors(); beforeInit != afterInit {
		t.Fatal("resetTelemetry replaced an active pre-init error subscription")
	}
	destination := make(chan frame)

	if observable.publishRx(context.Background(), destination, frame{}) {
		t.Fatal("publishRx should report a full unbuffered destination")
	}
	if got := observable.stats().SourceDropped; got != 1 {
		t.Fatalf("SourceDropped = %d, want 1", got)
	}
	select {
	case err := <-observable.errors():
		if !errors.Is(err, errRxOverflow) {
			t.Fatalf("error = %v, want errRxOverflow", err)
		}
	default:
		t.Fatal("expected an overflow error")
	}
}

func TestRxFanoutReportsSubscriberOverflowAndUnsubscribes(t *testing.T) {
	telemetry := newDriverTelemetry()
	fanout := &rxFanout{
		subs:      make(map[chan frame]struct{}),
		telemetry: telemetry,
	}
	received, unsubscribe := fanout.Subscribe(1)

	fanout.dispatch(frame{ID: 1})
	fanout.dispatch(frame{ID: 2})

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
