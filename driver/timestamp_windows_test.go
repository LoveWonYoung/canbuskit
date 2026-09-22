//go:build windows

package driver

import (
	"bytes"
	"log"
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

func TestPCANEchoLogUsesTXHardwareTimestamp(t *testing.T) {
	originalOutput := log.Writer()
	var output bytes.Buffer
	log.SetOutput(&output)
	SetPrintLog(true)
	t.Cleanup(func() {
		SetPrintLog(false)
		_ = SetLogFilter(LogFilterOff, nil)
		log.SetOutput(originalOutput)
	})

	driver := &PCAN{cfg: Config{IncludeTxEcho: false}}
	driver.enqueueMessage(0x123, 1, []byte{0xAA}, pcanMessageEcho, 1_002_003)

	got := output.String()
	if !strings.Contains(got, "TX CAN") {
		t.Fatalf("PCAN echo was not logged as TX: %q", got)
	}
	if !strings.Contains(got, "Timestamp=1s 002ms 003us") {
		t.Fatalf("PCAN TX hardware timestamp was not logged: %q", got)
	}
}
