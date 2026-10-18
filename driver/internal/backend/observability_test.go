package backend

import (
	"context"
	"errors"
	"testing"

	"github.com/LoveWonYoung/canbuskit/v2/can"
)

func TestPublishRxReportsSourceOverflow(t *testing.T) {
	var observable observability
	beforeInit := observable.errors()
	events := observable.prepare(can.Config{})
	if afterInit := observable.errors(); beforeInit != afterInit || events.errors != beforeInit {
		t.Fatal("prepare replaced the error channel")
	}
	destination := make(chan can.Frame)

	if observable.publishRx(context.Background(), destination, can.Frame{}) {
		t.Fatal("publishRx should report a full unbuffered destination")
	}
	if got := observable.stats().SourceDropped; got != 1 {
		t.Fatalf("SourceDropped = %d, want 1", got)
	}
	select {
	case err := <-observable.errors():
		if !errors.Is(err, can.ErrRxOverflow) {
			t.Fatalf("error = %v, want ErrRxOverflow", err)
		}
		overflow, ok := err.(*can.ReceiveOverflowError)
		if !ok || overflow.Stage != "source" {
			t.Fatalf("overflow = %#v, want stage source", err)
		}
	default:
		t.Fatal("expected an overflow error")
	}
}

func TestRxFanoutReportsSubscriberOverflowAndUnsubscribes(t *testing.T) {
	events := newTelemetry()
	fanout := &rxFanout{
		subs:      make(map[chan can.Frame]struct{}),
		telemetry: events,
	}
	received, unsubscribe := fanout.Subscribe(1)

	fanout.dispatch(can.Frame{ID: 1})
	fanout.dispatch(can.Frame{ID: 2})

	if got := events.stats().SubscriberDropped; got != 1 {
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
