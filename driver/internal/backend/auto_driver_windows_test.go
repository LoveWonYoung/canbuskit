//go:build windows

package backend

import (
	"errors"
	"github.com/LoveWonYoung/canbuskit/v2/can"
	"testing"
)

type autoTestDriver struct {
	initErr    error
	fd         bool
	stopped    bool
	initCall   int
	brsEnabled bool
}

func (d *autoTestDriver) initialize() error {
	d.initCall++
	return d.initErr
}
func (d *autoTestDriver) start() error          { return nil }
func (d *autoTestDriver) stop()                 { d.stopped = true }
func (d *autoTestDriver) write(can.Frame) error { return nil }
func (d *autoTestDriver) subscribe(int) (<-chan can.Frame, func()) {
	return nil, func() {}
}
func (d *autoTestDriver) isFD() bool           { return d.fd }
func (d *autoTestDriver) config() can.Config   { return can.Config{} }
func (d *autoTestDriver) setBRS(enabled bool)  { d.brsEnabled = enabled }
func (d *autoTestDriver) brs() bool            { return d.brsEnabled }
func (d *autoTestDriver) errors() <-chan error { return closedErrors }
func (d *autoTestDriver) stats() can.Stats     { return can.Stats{} }
func (d *autoTestDriver) busLoad() can.BusLoad { return can.BusLoad{} }

func TestAutoDriverCleansFailuresAndRejectsModeMismatch(t *testing.T) {
	failed := &autoTestDriver{initErr: errors.New("not available"), fd: true}
	wrongMode := &autoTestDriver{fd: false}
	selected := &autoTestDriver{fd: true}

	auto := newAutoDevice(
		can.DefaultConfig(can.FD, can.Channel2),
		autoCandidate{Name: "failed", New: func(can.Config) device { return failed }},
		autoCandidate{Name: "wrong-mode", New: func(can.Config) device { return wrongMode }},
		autoCandidate{Name: "selected", New: func(can.Config) device { return selected }},
	)
	if err := auto.initialize(); err != nil {
		t.Fatal(err)
	}
	if !failed.stopped {
		t.Fatal("failed candidate was not stopped")
	}
	if !wrongMode.stopped {
		t.Fatal("mode-mismatched candidate was not stopped")
	}
	if got := auto.selectedBackend(); got != "selected" {
		t.Fatalf("selectedBackend() = %q, want selected", got)
	}

	auto.stop()
	if !selected.stopped {
		t.Fatal("selected backend was not stopped")
	}
	if auto.selectedBackend() != "" {
		t.Fatal("selected name was not cleared on stop")
	}
}

func TestTSMasterDefaultMappingSeparatesApplicationAndHardwareChannel(t *testing.T) {
	cfg := can.DefaultConfig(can.FD, can.Channel4)
	dev := newTSMaster(cfg, TC1016, nil)
	mapping := dev.Mapping()

	if mapping.ApplicationChannel != can.Channel1 {
		t.Fatalf("application channel = %d, want can.Channel1", mapping.ApplicationChannel)
	}
	if mapping.HardwareIndex != 0 {
		t.Fatalf("hardware index = %d, want 0", mapping.HardwareIndex)
	}
	if mapping.HardwareChannel != can.Channel4 {
		t.Fatalf("hardware channel = %d, want can.Channel4", mapping.HardwareChannel)
	}
}

func TestTSMasterSetBRS(t *testing.T) {
	dev := newTSMaster(can.DefaultConfig(can.FD, can.Channel1), TC1016, nil)
	if dev.brs() {
		t.Fatal("BRS must be off by default")
	}
	dev.setBRS(true)
	if !dev.brs() || !dev.config().BRS {
		t.Fatal("setBRS(true) did not enable BRS")
	}
}

func TestAutoDriverSetBRSForwardsToSelectedDriver(t *testing.T) {
	selected := &autoTestDriver{fd: true}
	auto := newAutoDevice(
		can.DefaultConfig(can.FD, can.Channel1),
		autoCandidate{Name: "selected", New: func(can.Config) device { return selected }},
	)
	auto.setBRS(true)
	if !auto.brs() {
		t.Fatal("autoDevice.brs() before initialize should follow setBRS")
	}
	if err := auto.initialize(); err != nil {
		t.Fatal(err)
	}
	auto.setBRS(true)
	if !selected.brsEnabled {
		t.Fatal("setBRS was not forwarded to the selected backend")
	}
	auto.stop()
}
