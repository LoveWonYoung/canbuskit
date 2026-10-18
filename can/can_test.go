package can_test

import (
	"testing"

	"github.com/LoveWonYoung/canbuskit/v2/can"
)

func TestNewFrameCopiesPayloadAndSelectsDLC(t *testing.T) {
	payload := make([]byte, 9)
	frame, err := can.NewFrame(0x123, payload, can.FrameFlags{FD: true, BRS: true})
	if err != nil {
		t.Fatal(err)
	}
	if frame.DLC != 9 || frame.DataLength() != 12 {
		t.Fatalf("DLC/length = %d/%d, want 9/12", frame.DLC, frame.DataLength())
	}
	payload[0] = 0xFF
	if frame.Data[0] != 0 {
		t.Fatal("frame retained caller payload storage")
	}
}

func TestNewFrameRejectsInvalidFlagsAndLengths(t *testing.T) {
	if _, err := can.NewFrame(0x123, nil, can.FrameFlags{BRS: true}); err == nil {
		t.Fatal("classic frame with BRS was accepted")
	}
	if _, err := can.NewFrame(0x800, nil, can.FrameFlags{}); err == nil {
		t.Fatal("standard frame with 29-bit ID was accepted")
	}
	if _, err := can.NewFrame(0x1FFFFFFF, make([]byte, 64), can.FrameFlags{FD: true, Extended: true}); err != nil {
		t.Fatalf("valid extended FD frame rejected: %v", err)
	}
}

func TestConfigNormalization(t *testing.T) {
	cfg, err := (can.Config{Mode: can.Classic}).Normalized()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NominalBitrate != 500_000 || cfg.RxBufferSize != 1024 {
		t.Fatalf("unexpected normalized config: %+v", cfg)
	}
	if err := (can.Config{Mode: can.Mode(99)}).Validate(); err == nil {
		t.Fatal("invalid mode was accepted")
	}
	if err := (can.Config{Mode: can.Classic, BRS: true}).Validate(); err == nil {
		t.Fatal("classic mode with BRS was accepted")
	}
	if err := (can.Config{Mode: can.Classic, NominalTiming: can.BitTiming{BRP: 1, SEG1: 2, SEG2: 1, SJW: 1}}).Validate(); err == nil {
		t.Fatal("classic mode with explicit timing was accepted")
	}
	partial := can.Config{Mode: can.FD, NominalTiming: can.BitTiming{BRP: 1, SEG1: 2, SEG2: 1, SJW: 1}}
	if err := partial.Validate(); err == nil {
		t.Fatal("CAN FD timing with only one phase was accepted")
	}
	incomplete := can.Config{Mode: can.FD, NominalTiming: can.BitTiming{SEG1: 2}, DataTiming: can.BitTiming{BRP: 1, SEG1: 2, SEG2: 1, SJW: 1}}
	if err := incomplete.Validate(); err == nil {
		t.Fatal("partial bit timing was accepted")
	}
	explicit := can.Config{
		Mode:          can.FD,
		NominalTiming: can.BitTiming{BRP: 1, SEG1: 59, SEG2: 20, SJW: 2},
		DataTiming:    can.BitTiming{BRP: 1, SEG1: 14, SEG2: 5, SJW: 2},
	}
	if err := explicit.Validate(); err != nil {
		t.Fatalf("complete CAN FD timing was rejected: %v", err)
	}
}
