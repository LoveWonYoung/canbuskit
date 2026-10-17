package memory_test

import (
	"context"
	"testing"
	"time"

	"github.com/LoveWonYoung/canbuskit/v2/can"
	"github.com/LoveWonYoung/canbuskit/v2/can/memory"
)

func TestPairTransfersFrames(t *testing.T) {
	a, b := memory.Pair(can.Config{Mode: can.FD, IncludeTxEcho: true})
	ctx := context.Background()
	if err := a.Open(ctx); err != nil {
		t.Fatal(err)
	}
	if err := b.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	defer b.Close()

	rx, unsubscribe, err := b.Subscribe(1)
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	echo, unsubscribeEcho, err := a.Subscribe(1)
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribeEcho()

	frame, err := can.NewFrame(0x7E0, []byte{1, 2, 3}, can.FrameFlags{FD: true, BRS: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Send(ctx, frame); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-rx:
		if got.Direction != can.Receive || got.ID != frame.ID || string(got.Payload()) != string(frame.Payload()) {
			t.Fatalf("received frame = %+v, want %+v", got, frame)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for frame")
	}
	select {
	case got := <-echo:
		if got.Direction != can.Transmit {
			t.Fatalf("echo direction = %v", got.Direction)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for transmit echo")
	}
}

func TestClassicEndpointRejectsFDFrame(t *testing.T) {
	a, b := memory.Pair(can.DefaultConfig(can.Classic, can.Channel1))
	ctx := context.Background()
	if err := a.Open(ctx); err != nil {
		t.Fatal(err)
	}
	if err := b.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	defer b.Close()

	frame, err := can.NewFrame(0x123, []byte{1}, can.FrameFlags{FD: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Send(ctx, frame); err == nil {
		t.Fatal("classic endpoint accepted CAN FD frame")
	}
}
