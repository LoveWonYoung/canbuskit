//go:build windows

package driver

import "testing"

func TestPCANClassicTimestampUS(t *testing.T) {
	ts := pcanTimestamp{Millis: 23, MillisOverflow: 2, Micros: 456}
	want := uint64(456) + uint64(23)*1_000 + uint64(2)*0x1_0000_0000*1_000
	if got := pcanClassicTimestampUS(ts); got != want {
		t.Fatalf("pcanClassicTimestampUS() = %d, want %d", got, want)
	}
}

func TestTSMasterTimestampUS(t *testing.T) {
	if got := tsmasterTimestampUS(123); got != 123 {
		t.Fatalf("positive timestamp = %d, want 123", got)
	}
	if got := tsmasterTimestampUS(-1); got != 0 {
		t.Fatalf("negative timestamp = %d, want 0", got)
	}
}
