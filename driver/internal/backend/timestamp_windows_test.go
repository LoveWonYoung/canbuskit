//go:build windows

package backend

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

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

func TestCanalystTimestampUS(t *testing.T) {
	if got := canalystTimestampUS(123, 1); got != 12_300 {
		t.Fatalf("valid timestamp = %d, want 12300", got)
	}
	if got := canalystTimestampUS(123, 0); got != 0 {
		t.Fatalf("disabled timestamp = %d, want 0", got)
	}
	if got, want := canalystTimestampWrapUS(), (uint64(1)<<32)*100; got != want {
		t.Fatalf("timestamp wrap = %d, want %d", got, want)
	}
}

func TestPCANEchoLogUsesRelativeHardwareTimes(t *testing.T) {
	var output bytes.Buffer

	driver := &PCAN{cfg: config{IncludeTxEcho: false, Logger: slog.New(slog.NewTextHandler(&output, nil))}}
	driver.enqueueMessage(0x123, 1, []byte{0xAA}, pcanMessageEcho, 1_002_003)
	driver.enqueueMessage(0x123, 1, []byte{0xBB}, pcanMessageEcho, 1_003_507)

	got := output.String()
	if !strings.Contains(got, "TX CAN") {
		t.Fatalf("PCAN echo was not logged as TX: %q", got)
	}
	if !strings.Contains(got, "Elapsed=0s 000ms 000us, Delta=0s 000ms 000us") {
		t.Fatalf("PCAN first TX echo did not establish the relative origin: %q", got)
	}
	if !strings.Contains(got, "Elapsed=0s 001ms 504us, Delta=0s 001ms 504us") {
		t.Fatalf("PCAN TX echo relative times were not logged: %q", got)
	}
}

func TestPCANRelativeTimesIncludeFilteredFrames(t *testing.T) {
	var output bytes.Buffer

	driver := &PCAN{}
	driver.enqueueMessage(0x321, 1, []byte{0xAA}, pcanMessageEcho, 10_000)
	driver.cfg.Logger = slog.New(slog.NewTextHandler(&output, nil))
	driver.enqueueMessage(0x123, 1, []byte{0xBB}, pcanMessageEcho, 10_250)

	got := output.String()
	if !strings.Contains(got, "Elapsed=0s 000ms 250us, Delta=0s 000ms 250us") {
		t.Fatalf("filtered frame was not included in relative timing: %q", got)
	}
}
