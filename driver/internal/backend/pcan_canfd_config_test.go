//go:build windows

package backend

import (
	"github.com/LoveWonYoung/canbuskit/v2/can"
	"testing"
)

func TestPCANFDBitrateFromTiming(t *testing.T) {
	nominal := can.BitTiming{BRP: 20, SEG1: 5, SEG2: 2, SJW: 1}
	data := can.BitTiming{BRP: 4, SEG1: 7, SEG2: 2, SJW: 1}
	got := pcanFDBitrateFromTiming(nominal, data)
	want := pcanFDDefaultBitrate
	if got != want {
		t.Fatalf("pcanFDBitrateFromTiming() = %q, want %q", got, want)
	}
}

func TestPCANSetBRS(t *testing.T) {
	drv := newPCAN(can.DefaultConfig(can.FD, can.Channel1))
	if drv.brs() {
		t.Fatal("BRS must be off by default")
	}
	drv.setBRS(true)
	if !drv.brs() {
		t.Fatal("setBRS(true) did not enable BRS")
	}
	if !drv.config().BRS {
		t.Fatal("config().BRS not updated")
	}
	drv.setBRS(false)
	if drv.brs() {
		t.Fatal("setBRS(false) did not disable BRS")
	}
}
