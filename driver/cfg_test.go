package driver

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig(CANFD, CHANNEL2)
	if cfg.Mode != CANFD {
		t.Fatalf("Mode = %d, want CANFD", cfg.Mode)
	}
	if cfg.Channel != CHANNEL2 {
		t.Fatalf("Channel = %d, want %d", cfg.Channel, CHANNEL2)
	}
	if cfg.NominalBitrate != 500_000 || cfg.DataBitrate != 2_000_000 {
		t.Fatalf("unexpected default bitrates: %d/%d", cfg.NominalBitrate, cfg.DataBitrate)
	}
	if cfg.RxBufferSize != RxChannelBufferSize {
		t.Fatalf("RxBufferSize = %d, want %d", cfg.RxBufferSize, RxChannelBufferSize)
	}
	if cfg.PollingInterval != PollingInterval {
		t.Fatalf("PollingInterval = %s, want %s", cfg.PollingInterval, PollingInterval)
	}
	if cfg.IncludeTxEcho {
		t.Fatal("TX echo must be disabled by default")
	}
	if cfg.BRS {
		t.Fatal("BRS must be disabled by default")
	}
}

func TestNormalizeConfigFillsDefaults(t *testing.T) {
	cfg, err := normalizeConfig(Config{Mode: CAN, Channel: CHANNEL1})
	if err != nil {
		t.Fatal(err)
	}
	want := DefaultConfig(CAN, CHANNEL1)
	if cfg != want {
		t.Fatalf("normalizeConfig() = %+v, want %+v", cfg, want)
	}
}

func TestNormalizeConfigRejectsInvalidValues(t *testing.T) {
	tests := []Config{
		{Mode: CanType(99)},
		{Mode: CAN, RxBufferSize: -1},
		{Mode: CAN, PollingInterval: -time.Millisecond},
	}
	for _, cfg := range tests {
		if err := cfg.Validate(); err == nil {
			t.Fatalf("Validate(%+v) unexpectedly succeeded", cfg)
		}
		if _, err := normalizeConfig(cfg); err == nil {
			t.Fatalf("normalizeConfig(%+v) unexpectedly succeeded", cfg)
		}
	}
}

func TestValidateWriteStandardCANAndCANFD(t *testing.T) {
	canCfg := DefaultConfig(CAN, CHANNEL1)
	fdCfg := DefaultConfig(CANFD, CHANNEL1)

	valid := []struct {
		cfg  Config
		id   int32
		fd   bool
		data []byte
	}{
		{canCfg, 0, false, nil},
		{canCfg, 0x7FF, false, make([]byte, 8)},
		{fdCfg, 0x123, false, make([]byte, 8)},
		{fdCfg, 0x123, true, nil},
		{fdCfg, 0x123, true, make([]byte, 64)},
	}
	for _, tc := range valid {
		if err := validateWrite(tc.cfg, tc.id, tc.fd, tc.data); err != nil {
			t.Fatalf("validateWrite(%+v, 0x%X, %t, len=%d): %v", tc.cfg, tc.id, tc.fd, len(tc.data), err)
		}
	}

	invalid := []struct {
		cfg  Config
		id   int32
		fd   bool
		data []byte
	}{
		{canCfg, -1, false, nil},
		{canCfg, 0x800, false, nil},
		{canCfg, 0x123, true, nil},
		{canCfg, 0x123, false, make([]byte, 9)},
		{fdCfg, 0x123, true, make([]byte, 65)},
	}
	for _, tc := range invalid {
		if err := validateWrite(tc.cfg, tc.id, tc.fd, tc.data); err == nil {
			t.Fatalf("validateWrite(%+v, 0x%X, %t, len=%d) unexpectedly succeeded", tc.cfg, tc.id, tc.fd, len(tc.data))
		}
	}
}

func TestLogFilterList(t *testing.T) {
	originalOutput := log.Writer()
	var output bytes.Buffer
	log.SetOutput(&output)
	SetPrintLog(true)
	t.Cleanup(func() {
		SetPrintLog(false)
		_ = SetLogFilter(LogFilterOff, nil)
		log.SetOutput(originalOutput)
	})

	if err := SetLogFilter(LogFilterList, []uint32{0x123, 0x456}); err != nil {
		t.Fatal(err)
	}
	logCANMessage("RX", 0x123, 1, []byte{0xAA}, CAN)
	logCANMessage("RX", 0x321, 1, []byte{0xBB}, CAN)

	got := output.String()
	if !strings.Contains(got, "ID=0x123") {
		t.Fatalf("allowed CAN ID was not logged: %q", got)
	}
	if strings.Contains(got, "ID=0x321") {
		t.Fatalf("filtered CAN ID was logged: %q", got)
	}
}

func TestLogFilterConfiguration(t *testing.T) {
	t.Cleanup(func() {
		_ = SetLogFilter(LogFilterOff, nil)
	})

	if err := SetLogFilter(LogFilterList, nil); err != nil {
		t.Fatal(err)
	}
	if filter := logFilter.Load(); filter == nil || filter.allows(0x123) {
		t.Fatal("empty list should suppress every CAN ID")
	}
	if err := SetLogFilter(LogFilterList, []uint32{0x800}); err == nil {
		t.Fatal("out-of-range CAN ID unexpectedly accepted")
	}
	if err := SetLogFilter(LogFilterMode(99), nil); err == nil {
		t.Fatal("unsupported filter mode unexpectedly accepted")
	}
	if err := SetLogFilter(LogFilterOff, nil); err != nil {
		t.Fatal(err)
	}
	if filter := logFilter.Load(); filter != nil {
		t.Fatal("LogFilterOff should remove the active filter")
	}
}

func TestDriverLifecycleWaitsForReadLoopAndIsIdempotent(t *testing.T) {
	var lifecycle driverLifecycle
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	exited := make(chan struct{})

	lifecycle.markInitialized()
	if !lifecycle.start(func() {
		close(started)
		<-ctx.Done()
		close(exited)
	}) {
		t.Fatal("first start failed")
	}
	<-started
	if lifecycle.start(func() {}) {
		t.Fatal("second concurrent start unexpectedly succeeded")
	}

	if !lifecycle.cancelAndWait(cancel) {
		t.Fatal("stop did not report initialized state")
	}
	select {
	case <-exited:
	default:
		t.Fatal("cancelAndWait returned before read loop exited")
	}
	if lifecycle.cancelAndWait(cancel) {
		t.Fatal("second stop unexpectedly reported initialized state")
	}
	if lifecycle.start(func() {}) {
		t.Fatal("start after stop unexpectedly succeeded without Init")
	}
}
