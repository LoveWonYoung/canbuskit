package backend

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/LoveWonYoung/canbuskit/v2/can"
)

func TestFrameValidDataLength(t *testing.T) {
	tests := []struct {
		name  string
		frame can.Frame
		want  int
		valid bool
	}{
		{name: "classic", frame: can.Frame{DLC: 8}, want: 8, valid: true},
		{name: "classic invalid", frame: can.Frame{DLC: 9}, valid: false},
		{name: "fd", frame: can.Frame{DLC: 15, FD: true}, want: 64, valid: true},
		{name: "fd invalid", frame: can.Frame{DLC: 16, FD: true}, valid: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, valid := tc.frame.ValidDataLength()
			if got != tc.want || valid != tc.valid {
				t.Fatalf("ValidDataLength() = (%d, %t), want (%d, %t)", got, valid, tc.want, tc.valid)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := can.DefaultConfig(can.FD, can.Channel2)
	if cfg.Mode != can.FD {
		t.Fatalf("Mode = %d, want FD", cfg.Mode)
	}
	if cfg.Channel != can.Channel2 {
		t.Fatalf("Channel = %d, want %d", cfg.Channel, can.Channel2)
	}
	if cfg.NominalBitrate != 500_000 || cfg.DataBitrate != 2_000_000 {
		t.Fatalf("unexpected default bitrates: %d/%d", cfg.NominalBitrate, cfg.DataBitrate)
	}
	if cfg.RxBufferSize != 1024 {
		t.Fatalf("RxBufferSize = %d, want 1024", cfg.RxBufferSize)
	}
	if cfg.PollingInterval != time.Millisecond {
		t.Fatalf("PollingInterval = %s, want %s", cfg.PollingInterval, time.Millisecond)
	}
	if cfg.IncludeTxEcho {
		t.Fatal("TX echo must be disabled by default")
	}
	if cfg.BRS {
		t.Fatal("BRS must be disabled by default")
	}
}

func TestNormalizeConfigFillsDefaults(t *testing.T) {
	cfg, err := normalizeConfig(can.Config{Mode: can.Classic, Channel: can.Channel1})
	if err != nil {
		t.Fatal(err)
	}
	want := can.DefaultConfig(can.Classic, can.Channel1)
	if cfg != want {
		t.Fatalf("normalizeConfig() = %+v, want %+v", cfg, want)
	}
}

func TestNormalizeConfigRejectsInvalidValues(t *testing.T) {
	tests := []can.Config{
		{Mode: can.Mode(99)},
		{Mode: can.Classic, RxBufferSize: -1},
		{Mode: can.Classic, PollingInterval: -time.Millisecond},
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

func TestInjectedFrameLogger(t *testing.T) {
	var output bytes.Buffer
	cfg := can.Config{Logger: slog.New(slog.NewTextHandler(&output, nil))}
	logCANMessageRelative(cfg, "RX", 0x123, 1, []byte{0xAA}, can.Classic, 1_002_003, 500)

	got := output.String()
	if !strings.Contains(got, "ID=0x123") {
		t.Fatalf("allowed CAN ID was not logged: %q", got)
	}
	if !strings.Contains(got, "Elapsed=1s 002ms 003us, Delta=0s 000ms 500us") {
		t.Fatalf("relative hardware times were not logged in s/ms/us format: %q", got)
	}
}

func TestNilLoggerDisablesLogging(t *testing.T) {
	logCANMessageRelative(can.Config{}, "RX", 0x123, 1, []byte{0xAA}, can.Classic, 0, 0)
}

func TestLifecycleWaitsForReadLoopAndIsIdempotent(t *testing.T) {
	var life lifecycle
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	exited := make(chan struct{})

	life.markInitialized()
	if !life.start(func() {
		close(started)
		<-ctx.Done()
		close(exited)
	}) {
		t.Fatal("first start failed")
	}
	<-started
	if life.start(func() {}) {
		t.Fatal("second concurrent start unexpectedly succeeded")
	}

	if !life.cancelAndWait(cancel) {
		t.Fatal("stop did not report initialized state")
	}
	select {
	case <-exited:
	default:
		t.Fatal("cancelAndWait returned before read loop exited")
	}
	if life.cancelAndWait(cancel) {
		t.Fatal("second stop unexpectedly reported initialized state")
	}
	if life.start(func() {}) {
		t.Fatal("start after stop unexpectedly succeeded without initialize")
	}
}
