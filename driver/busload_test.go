package driver

import (
	"testing"
	"time"
)

func TestClassicCANFrameBitsIncludesIFS(t *testing.T) {
	frame := CanFrame{ID: 0x155, DLC: 2, Data: [64]byte{0x55, 0xAA}}
	got := classicCANFrameBits(frame)
	unstuffed := 47 + 8*2
	if got < unstuffed {
		t.Fatalf("classicCANFrameBits() = %d, want >= %d", got, unstuffed)
	}
	if got > 55+10*2 {
		t.Fatalf("classicCANFrameBits() = %d, exceeds worst-case 55+10n", got)
	}
}

func TestClassicCANFrameBitsStuffsLongZeroRuns(t *testing.T) {
	empty := classicCANFrameBits(CanFrame{ID: 0, DLC: 0})
	if empty <= 47 {
		t.Fatalf("zero ID empty frame = %d bits, want stuffing above 47", empty)
	}
}

func TestCANFDFrameBitsWorstCase(t *testing.T) {
	frame := CanFrame{ID: 0x100, DLC: 8, IsFD: true, Data: [64]byte{1, 2, 3, 4, 5, 6, 7, 8}}
	arb, data := canFDFrameBits(frame)
	if data != 0 {
		t.Fatalf("data-phase bits = %d, want 0 (no BRS)", data)
	}
	want := (1 + 11 + 17 + 5 + 12 + 64) * 5 / 4
	if arb != want {
		t.Fatalf("canFDFrameBits() = %d, want %d", arb, want)
	}
}

func TestCANFDFrameBitsWithBRSUsesDataPhase(t *testing.T) {
	frame := CanFrame{ID: 0x100, DLC: 8, IsFD: true, BRS: true, Data: [64]byte{1, 2, 3, 4, 5, 6, 7, 8}}
	arb, data := canFDFrameBits(frame)
	if data == 0 {
		t.Fatal("expected data-phase bits when BRS is set")
	}
	noBRS := CanFrame{ID: 0x100, DLC: 15, IsFD: true}
	withBRS := noBRS
	withBRS.BRS = true
	slow := frameOccupancy(noBRS, 500_000, 2_000_000)
	fast := frameOccupancy(withBRS, 500_000, 2_000_000)
	if fast >= slow {
		t.Fatalf("BRS occupancy %s, want less than %s", fast, slow)
	}
	if arb+data != (1+11+17+5+12+64)*5/4 {
		t.Fatalf("arb(%d)+data(%d) mismatch", arb, data)
	}
}

func TestBusLoadWindowOccupancy(t *testing.T) {
	var meter busLoadMeter
	meter.configure(Config{NominalBitrate: 500_000, DataBitrate: 2_000_000})

	now := time.Unix(0, 0)
	frame := CanFrame{ID: 0x155, DLC: 0}
	meter.recordTx(0x155, false, false, nil, now)

	got := meter.snapshot(now.Add(time.Second))
	if got.FrameCount != 1 {
		t.Fatalf("FrameCount = %d, want 1", got.FrameCount)
	}
	if got.NominalBitrate != 500_000 {
		t.Fatalf("NominalBitrate = %d, want 500000", got.NominalBitrate)
	}
	want := float64(frameOccupancy(frame, 500_000, 2_000_000)) / float64(time.Second)
	if got.Load < want*0.99 || got.Load > want*1.01 {
		t.Fatalf("Load = %v, want ~%v", got.Load, want)
	}
}

func TestBusLoadDedupsTxEcho(t *testing.T) {
	var meter busLoadMeter
	meter.configure(Config{NominalBitrate: 500_000})
	now := time.Unix(0, 0)
	data := []byte{0x11, 0x22}

	meter.recordTx(0x123, false, false, data, now)
	meter.observe(CanFrame{Direction: TX, ID: 0x123, DLC: 2, Data: [64]byte{0x11, 0x22}}, now.Add(time.Millisecond))
	meter.observe(CanFrame{Direction: RX, ID: 0x123, DLC: 2, Data: [64]byte{0x11, 0x22}}, now.Add(2*time.Millisecond))

	got := meter.snapshot(now.Add(time.Second))
	if got.FrameCount != 1 {
		t.Fatalf("FrameCount = %d, want 1 after TX echo", got.FrameCount)
	}

	meter.observe(CanFrame{Direction: RX, ID: 0x200, DLC: 0}, now.Add(3*time.Millisecond))
	got = meter.snapshot(now.Add(time.Second))
	if got.FrameCount != 2 {
		t.Fatalf("FrameCount = %d, want 2 after unrelated RX", got.FrameCount)
	}
}

func TestDriverObservabilityBusLoad(t *testing.T) {
	var observable driverObservability
	observable.resetTelemetryWith(Config{NominalBitrate: 500_000, DataBitrate: 2_000_000})
	observable.recordBusTx(0x100, false, false, []byte{0x01})
	observable.observeBusFrame(CanFrame{Direction: RX, ID: 0x200, DLC: 0})

	got := observable.BusLoad()
	if got.FrameCount != 2 {
		t.Fatalf("BusLoad FrameCount = %d, want 2", got.FrameCount)
	}
	if got.Load <= 0 {
		t.Fatal("expected non-zero bus load")
	}
}
