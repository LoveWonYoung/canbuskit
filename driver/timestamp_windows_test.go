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

func TestPCANEchoLogUsesRelativeHardwareTimes(t *testing.T) {
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
	originalOutput := log.Writer()
	var output bytes.Buffer
	log.SetOutput(&output)
	SetPrintLog(true)
	if err := SetLogFilter(LogFilterList, []uint32{0x123}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		SetPrintLog(false)
		_ = SetLogFilter(LogFilterOff, nil)
		log.SetOutput(originalOutput)
	})

	driver := &PCAN{}
	driver.enqueueMessage(0x321, 1, []byte{0xAA}, pcanMessageEcho, 10_000)
	driver.enqueueMessage(0x123, 1, []byte{0xBB}, pcanMessageEcho, 10_250)

	got := output.String()
	if strings.Contains(got, "ID=0x321") {
		t.Fatalf("filtered frame was logged: %q", got)
	}
	if !strings.Contains(got, "Elapsed=0s 000ms 250us, Delta=0s 000ms 250us") {
		t.Fatalf("filtered frame was not included in relative timing: %q", got)
	}
}
