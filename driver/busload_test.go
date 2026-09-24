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

func TestClassicCANFrameBitsMatchesReferenceImplementation(t *testing.T) {
	for id := uint32(0); id <= 0x7FF; id += 37 {
		for dlc := byte(0); dlc <= 8; dlc++ {
			frame := CanFrame{ID: id, DLC: dlc}
			for i := range frame.Data {
				frame.Data[i] = byte(uint32(i)*29 + id)
			}
			if got, want := classicCANFrameBits(frame), referenceClassicCANFrameBits(frame); got != want {
				t.Fatalf("ID=0x%03X DLC=%d: got %d bits, want %d", id, dlc, got, want)
			}
		}
	}
}

func TestClassicCANFrameBitsDoesNotAllocate(t *testing.T) {
	frame := CanFrame{ID: 0x123, DLC: 8, Data: [64]byte{1, 2, 3, 4, 5, 6, 7, 8}}
	if allocs := testing.AllocsPerRun(1000, func() { _ = classicCANFrameBits(frame) }); allocs != 0 {
		t.Fatalf("classicCANFrameBits allocated %.1f objects per call", allocs)
	}
}

func referenceClassicCANFrameBits(frame CanFrame) int {
	bits := make([]byte, 0, 98)
	appendBits := func(value uint32, n int) {
		for i := n - 1; i >= 0; i-- {
			bits = append(bits, byte((value>>i)&1))
		}
	}
	appendBits(0, 1)
	appendBits(frame.ID, 11)
	appendBits(0, 3)
	appendBits(uint32(frame.DLC), 4)
	for i := 0; i < int(frame.DLC); i++ {
		appendBits(uint32(frame.Data[i]), 8)
	}
	var crc uint16
	for _, bit := range bits {
		msb := byte((crc >> 14) & 1)
		crc = (crc << 1) & 0x7FFF
		if msb^bit == 1 {
			crc ^= 0x4599
		}
	}
	appendBits(uint32(crc), 15)

	stuffed := 0
	run := 0
	previous := byte(2)
	for _, bit := range bits {
		stuffed++
		if bit == previous {
			run++
		} else {
			previous = bit
			run = 1
		}
		if run == 5 {
			stuffed++
			previous ^= 1
			run = 1
		}
	}
	return stuffed + 13
}

func TestCANFDFrameBitsIncludesDLCAndUnstuffedTrailer(t *testing.T) {
	frame := CanFrame{ID: 0x100, DLC: 8, IsFD: true, Data: [64]byte{1, 2, 3, 4, 5, 6, 7, 8}}
	arb, data := canFDFrameBits(frame)
	if data != 0 {
		t.Fatalf("data-phase bits = %d, want 0 (no BRS)", data)
	}
	// 1+11+5+4+64+17 = 102 stuffable * 5/4 = 127, +13 trailer = 140.
	if arb != 140 {
		t.Fatalf("canFDFrameBits() = %d, want 140", arb)
	}
}

func TestCANFDCRCLength(t *testing.T) {
	short := CanFrame{ID: 0x100, DLC: 10, IsFD: true} // 16 bytes, CRC-17
	long := CanFrame{ID: 0x100, DLC: 11, IsFD: true}  // 20 bytes, CRC-21
	shortBits, _ := canFDFrameBits(short)
	longBits, _ := canFDFrameBits(long)
	if longBits <= shortBits {
		t.Fatalf("20-byte frame bits %d should exceed 16-byte %d", longBits, shortBits)
	}
}

func TestCANFDFrameBitsWithBRSUsesDataPhase(t *testing.T) {
	frame := CanFrame{ID: 0x100, DLC: 8, IsFD: true, BRS: true, Data: [64]byte{1, 2, 3, 4, 5, 6, 7, 8}}
	arb, data := canFDFrameBits(frame)
	if data == 0 {
		t.Fatal("expected data-phase bits when BRS is set")
	}
	if arb == 0 {
		t.Fatal("expected arbitration-phase bits when BRS is set")
	}
	noBRS := CanFrame{ID: 0x100, DLC: 15, IsFD: true}
	withBRS := noBRS
	withBRS.BRS = true
	slow := frameOccupancy(noBRS, 500_000, 2_000_000)
	fast := frameOccupancy(withBRS, 500_000, 2_000_000)
	if fast >= slow {
		t.Fatalf("BRS occupancy %s, want less than %s", fast, slow)
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

func TestBusLoadUsesHardwareFrameTimes(t *testing.T) {
	var meter busLoadMeter
	meter.configure(Config{NominalBitrate: 500_000})
	now := time.Unix(0, 0)
	meter.observe(CanFrame{Direction: RX, ID: 0x100, TimestampUS: 1_000_000}, now)
	// Both frames reached the read loop together, but occurred 1.1 s apart on the bus.
	meter.observe(CanFrame{Direction: RX, ID: 0x101, TimestampUS: 2_100_000}, now)
	if got := meter.snapshot(now).FrameCount; got != 1 {
		t.Fatalf("FrameCount = %d, want only the recent hardware-timed frame", got)
	}
}

func TestBusLoadStartupWindowUsesHardwareElapsedTime(t *testing.T) {
	var meter busLoadMeter
	meter.configure(Config{NominalBitrate: 500_000})
	now := time.Unix(0, 0)
	meter.observe(CanFrame{Direction: RX, ID: 0x100, TimestampUS: 1_000_000}, now)
	meter.observe(CanFrame{Direction: RX, ID: 0x101, TimestampUS: 1_500_000}, now)
	if got := meter.snapshot(now); got.Window != 500*time.Millisecond {
		t.Fatalf("Window = %s, want 500ms from hardware timestamps", got.Window)
	}
}

func TestBusLoadMovesTxToEchoTimestamp(t *testing.T) {
	var meter busLoadMeter
	meter.configure(Config{NominalBitrate: 500_000})
	now := time.Unix(0, 0)
	meter.observe(CanFrame{Direction: RX, ID: 0x100, TimestampUS: 1_000_000}, now)
	meter.recordTx(0x123, false, false, []byte{0x11}, now.Add(900*time.Millisecond))
	meter.observe(CanFrame{Direction: TX, ID: 0x123, DLC: 1, Data: [64]byte{0x11}, TimestampUS: 1_050_000}, now.Add(901*time.Millisecond))
	meter.observe(CanFrame{Direction: RX, ID: 0x200, TimestampUS: 2_000_000}, now.Add(950*time.Millisecond))
	if got := meter.snapshot(now.Add(1100 * time.Millisecond)).FrameCount; got != 1 {
		t.Fatalf("FrameCount = %d, want only the latest RX after the TX echo aged out", got)
	}
}

func TestBusLoadFallsBackWithoutHardwareTimestamp(t *testing.T) {
	var meter busLoadMeter
	meter.configure(Config{NominalBitrate: 500_000})
	now := time.Unix(0, 0)
	meter.recordTx(0x123, false, false, nil, now)
	meter.observe(CanFrame{Direction: TX, ID: 0x123}, now.Add(time.Millisecond))
	meter.observe(CanFrame{Direction: RX, ID: 0x200}, now.Add(2*time.Millisecond))
	if got := meter.snapshot(now.Add(3 * time.Millisecond)).FrameCount; got != 2 {
		t.Fatalf("FrameCount = %d, want TX and RX counted once with host timestamps", got)
	}
}

func TestBusLoadHardwareTimestampWrap(t *testing.T) {
	var meter busLoadMeter
	meter.configure(Config{NominalBitrate: 500_000})
	now := time.Unix(0, 0)
	meter.observeWithWrap(CanFrame{Direction: RX, ID: 0x100, TimestampUS: 999_900}, now, 1_000_000)
	meter.observeWithWrap(CanFrame{Direction: RX, ID: 0x101, TimestampUS: 100}, now.Add(200*time.Microsecond), 1_000_000)
	if got := meter.snapshot(now.Add(200 * time.Microsecond)).FrameCount; got != 2 {
		t.Fatalf("FrameCount = %d, want both frames across hardware clock wrap", got)
	}
}

func TestBusLoadMatchesEchoBeforeWriteReturns(t *testing.T) {
	var meter busLoadMeter
	meter.configure(Config{NominalBitrate: 500_000})
	now := time.Unix(0, 0)
	meter.observe(CanFrame{Direction: TX, ID: 0x123, DLC: 1, Data: [64]byte{0x11}, TimestampUS: 1_000_000}, now)
	meter.recordTx(0x123, false, false, []byte{0x11}, now.Add(time.Millisecond))
	if got := meter.snapshot(now.Add(2 * time.Millisecond)).FrameCount; got != 1 {
		t.Fatalf("FrameCount = %d, want one hardware-timed TX echo", got)
	}
	if got := meter.slots[0].frames; got != 0 {
		t.Fatalf("host-timed TX frames = %d, want 0 after matching early echo", got)
	}
}

func TestBusLoadMatchesDelayedTxEcho(t *testing.T) {
	var meter busLoadMeter
	meter.configure(Config{NominalBitrate: 500_000})
	now := time.Unix(0, 0)
	meter.recordTx(0x123, false, false, nil, now)
	meter.observe(CanFrame{Direction: TX, ID: 0x123, TimestampUS: 1_200_000}, now.Add(200*time.Millisecond))
	if got := meter.snapshot(now.Add(200 * time.Millisecond)).FrameCount; got != 1 {
		t.Fatalf("FrameCount = %d, want one confirmed TX", got)
	}
	if got := meter.slots[0].frames; got != 0 {
		t.Fatalf("host-timed TX frames = %d, want 0 after delayed echo", got)
	}
}

func TestBusLoadAgesHardwareWindowBeforeDelayedFrame(t *testing.T) {
	var meter busLoadMeter
	meter.configure(Config{NominalBitrate: 500_000})
	now := time.Unix(0, 0)
	meter.observe(CanFrame{Direction: RX, ID: 0x100, TimestampUS: 1_000_000}, now)
	// The second frame is delivered late; its device timestamp advanced only 100 ms.
	meter.observe(CanFrame{Direction: RX, ID: 0x101, TimestampUS: 1_100_000}, now.Add(1100*time.Millisecond))
	if got := meter.snapshot(now.Add(1100 * time.Millisecond)).FrameCount; got != 1 {
		t.Fatalf("FrameCount = %d, want old frame expired before delayed delivery", got)
	}
}

func TestBusLoadSteadyHardwareWindowDoesNotDropPartialBucket(t *testing.T) {
	var meter busLoadMeter
	meter.configure(Config{NominalBitrate: 500_000, DataBitrate: 2_000_000})
	start := time.Unix(0, 0)
	for i := 0; i < 200; i++ {
		at := time.Duration(i) * 10 * time.Millisecond
		meter.observe(CanFrame{Direction: RX, ID: 0x123, TimestampUS: 1_000_000 + uint64(at/time.Microsecond)}, start.Add(at))
	}
	// The exact one-second window (1.015s, 2.015s] contains frames at 1.02s..1.99s.
	got := meter.snapshot(start.Add(2015 * time.Millisecond))
	if got.FrameCount != 98 {
		t.Fatalf("FrameCount = %d, want 98 frames in the most recent second", got.FrameCount)
	}
}
