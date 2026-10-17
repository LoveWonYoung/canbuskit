package uds_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/LoveWonYoung/canbuskit/v2/can"
	"github.com/LoveWonYoung/canbuskit/v2/can/memory"
	"github.com/LoveWonYoung/canbuskit/v2/isotp"
	"github.com/LoveWonYoung/canbuskit/v2/uds"
)

func TestClientRequestOverMemoryBus(t *testing.T) {
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

	testerAddress, _ := isotp.NewAddress(0x7E0, 0x7E8)
	ecuAddress, _ := isotp.NewAddress(0x7E8, 0x7E0)
	cfg := isotp.DefaultConfig()
	cfg.STMin = 0
	client, err := uds.New(testerBus, testerAddress, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ecu, err := isotp.New(ecuBus, ecuAddress, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer ecu.Close()

	request := []byte{0x22, 0xF1, 0x90}
	response := append([]byte{0x62}, bytes.Repeat([]byte{0xA5}, 31)...)
	go func() {
		select {
		case got := <-ecu.RecvChan():
			if bytes.Equal(got, request) {
				_ = ecu.SendContext(ctx, response)
			}
		case <-time.After(time.Second):
		}
	}()

	got, err := client.RequestWithTimeout(request, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, response) {
		t.Fatalf("response mismatch: got % X, want % X", got, response)
	}
}
