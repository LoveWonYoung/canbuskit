package isotp_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/LoveWonYoung/canbuskit/v2/can"
	"github.com/LoveWonYoung/canbuskit/v2/can/memory"
	"github.com/LoveWonYoung/canbuskit/v2/isotp"
)

func TestSessionsExchangeMultiFramePayloadOverMemoryBus(t *testing.T) {
	ctx := context.Background()
	testerBus, ecuBus := memory.Pair(can.DefaultConfig(can.Classic, can.Channel1))
	if err := testerBus.Open(ctx); err != nil {
		t.Fatal(err)
	}
	if err := ecuBus.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer testerBus.Close()
	defer ecuBus.Close()

	testerAddress, _ := isotp.NewAddress(0x700, 0x708)
	ecuAddress, _ := isotp.NewAddress(0x708, 0x700)
	cfg := isotp.DefaultConfig()
	cfg.STMin = 0
	tester, err := isotp.New(testerBus, testerAddress, cfg)
	if err != nil {
		t.Fatal(err)
	}
	ecu, err := isotp.New(ecuBus, ecuAddress, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()
	defer ecu.Close()

	want := bytes.Repeat([]byte{0x5A}, 100)
	if err := tester.SendContext(ctx, want); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-ecu.RecvChan():
		if !bytes.Equal(got, want) {
			t.Fatalf("payload mismatch: got %d bytes", len(got))
		}
	case err := <-tester.Errors():
		t.Fatalf("tester ISO-TP error: %v", err)
	case err := <-ecu.Errors():
		t.Fatalf("ECU ISO-TP error: %v", err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for reassembled payload")
	}
}
