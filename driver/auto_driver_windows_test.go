//go:build windows

package driver

import (
	"errors"
	"testing"
)

type autoTestDriver struct {
	initErr  error
	fd       bool
	stopped  bool
	initCall int
}

func (d *autoTestDriver) Init() error {
	d.initCall++
	return d.initErr
}
func (d *autoTestDriver) Start()                          {}
func (d *autoTestDriver) Stop()                           { d.stopped = true }
func (d *autoTestDriver) Write(int32, bool, []byte) error { return nil }
func (d *autoTestDriver) RxChan() <-chan CanFrame         { return nil }
func (d *autoTestDriver) IsFDMode() bool                  { return d.fd }

func TestAutoDriverCleansFailuresAndRejectsModeMismatch(t *testing.T) {
	failed := &autoTestDriver{initErr: errors.New("not available"), fd: true}
	wrongMode := &autoTestDriver{fd: false}
	selected := &autoTestDriver{fd: true}

	auto := NewAutoDriverWithConfig(
		DefaultConfig(CANFD, CHANNEL2),
		AutoCandidate{Name: "failed", New: func(Config) CANDriver { return failed }},
		AutoCandidate{Name: "wrong-mode", New: func(Config) CANDriver { return wrongMode }},
		AutoCandidate{Name: "selected", New: func(Config) CANDriver { return selected }},
	)
	if err := auto.Init(); err != nil {
		t.Fatal(err)
	}
	if !failed.stopped {
		t.Fatal("failed candidate was not stopped")
	}
	if !wrongMode.stopped {
		t.Fatal("mode-mismatched candidate was not stopped")
	}
	if got := auto.SelectedName(); got != "selected" {
		t.Fatalf("SelectedName() = %q, want selected", got)
	}

	auto.Stop()
	if !selected.stopped {
		t.Fatal("selected driver was not stopped")
	}
	if auto.SelectedName() != "" {
		t.Fatal("selected name was not cleared on Stop")
	}
}

func TestTSMasterDefaultMappingSeparatesApplicationAndHardwareChannel(t *testing.T) {
	cfg := DefaultConfig(CANFD, CHANNEL4)
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
