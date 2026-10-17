//go:build windows

package backend

import (
	"errors"
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
func (d *autoTestDriver) start() error                    { return nil }
func (d *autoTestDriver) stop()                           { d.stopped = true }
func (d *autoTestDriver) write(int32, bool, []byte) error { return nil }
func (d *autoTestDriver) subscribe(int) (<-chan frame, func()) {
	return nil, func() {}
}
func (d *autoTestDriver) isFD() bool           { return d.fd }
func (d *autoTestDriver) config() config       { return config{} }
func (d *autoTestDriver) setBRS(enabled bool)  { d.brsEnabled = enabled }
func (d *autoTestDriver) brs() bool            { return d.brsEnabled }
func (d *autoTestDriver) errors() <-chan error { return closedDriverErrors }
func (d *autoTestDriver) stats() driverStats   { return driverStats{} }
func (d *autoTestDriver) busLoad() busLoadSnapshot { return busLoadSnapshot{} }

func TestAutoDriverCleansFailuresAndRejectsModeMismatch(t *testing.T) {
	failed := &autoTestDriver{initErr: errors.New("not available"), fd: true}
	wrongMode := &autoTestDriver{fd: false}
	selected := &autoTestDriver{fd: true}

	auto := newAutoDevice(
		defaultConfig(CANFD, CHANNEL2),
		autoCandidate{Name: "failed", New: func(config) device { return failed }},
		autoCandidate{Name: "wrong-mode", New: func(config) device { return wrongMode }},
		autoCandidate{Name: "selected", New: func(config) device { return selected }},
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
		t.Fatal("selected driver was not stopped")
	}
	if auto.selectedBackend() != "" {
		t.Fatal("selected name was not cleared on Stop")
	}
}

func TestTSMasterDefaultMappingSeparatesApplicationAndHardwareChannel(t *testing.T) {
	cfg := defaultConfig(CANFD, CHANNEL4)
	dev := NewTSMasterWithConfig(cfg, TC1016)
	mapping := dev.Mapping()

	if mapping.ApplicationChannel != CHANNEL1 {
		t.Fatalf("application channel = %d, want CHANNEL1", mapping.ApplicationChannel)
	}
	if mapping.HardwareIndex != 0 {
		t.Fatalf("hardware index = %d, want 0", mapping.HardwareIndex)
	}
	if mapping.HardwareChannel != CHANNEL4 {
		t.Fatalf("hardware channel = %d, want CHANNEL4", mapping.HardwareChannel)
	}
}

func TestTSMasterSetBRS(t *testing.T) {
	dev := NewTSMasterWithConfig(defaultConfig(CANFD, CHANNEL1), TC1016)
	if dev.brs() {
		t.Fatal("BRS must be off by default")
	}
	dev.setBRS(true)
	if !dev.brs() || !dev.config().BRS {
		t.Fatal("SetBRS(true) did not enable BRS")
	}
}

func TestAutoDriverSetBRSForwardsToSelectedDriver(t *testing.T) {
	selected := &autoTestDriver{fd: true}
	auto := newAutoDevice(
		defaultConfig(CANFD, CHANNEL1),
		autoCandidate{Name: "selected", New: func(config) device { return selected }},
	)
	auto.setBRS(true)
	if !auto.brs() {
		t.Fatal("autoDevice.brs() before Init should follow SetBRS")
	}
	if err := auto.initialize(); err != nil {
		t.Fatal(err)
	}
	auto.setBRS(true)
	if !selected.brsEnabled {
		t.Fatal("SetBRS was not forwarded to the selected driver")
	}
	auto.stop()
}
