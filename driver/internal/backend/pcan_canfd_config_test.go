//go:build windows

package backend

import "testing"

func TestPCANFDBitrateFromTiming(t *testing.T) {
	cfg := CANFDInitConfig{
		NBT_BRP: 20, NBT_SEG1: 5, NBT_SEG2: 2, NBT_SJW: 1,
		DBT_BRP: 4, DBT_SEG1: 7, DBT_SEG2: 2, DBT_SJW: 1,
	}
	got := pcanFDBitrateFromTiming(cfg)
	want := pcanFDDefaultBitrate
	if got != want {
		t.Fatalf("pcanFDBitrateFromTiming() = %q, want %q", got, want)
	}
}

func TestPCANSetCANFDInitConfig(t *testing.T) {
	drv := NewPCAN(CANFD, CHANNEL1)
	if drv.hasCANFDTiming {
		t.Fatal("expected no custom CAN FD timing by default")
	}
	cfg := CANFDInitConfig{
		NBT_BRP: 10, NBT_SEG1: 8, NBT_SEG2: 3, NBT_SJW: 2,
		DBT_BRP: 2, DBT_SEG1: 6, DBT_SEG2: 1, DBT_SJW: 1,
	}
	drv.SetCANFDInitConfig(cfg)
	if !drv.hasCANFDTiming {
		t.Fatal("expected custom CAN FD timing after SetCANFDInitConfig")
	}
	if drv.canFDTiming != cfg {
		t.Fatalf("canFDTiming = %+v, want %+v", drv.canFDTiming, cfg)
	}
}

func TestPCANSetBRS(t *testing.T) {
	drv := NewPCAN(CANFD, CHANNEL1)
	if drv.brs() {
		t.Fatal("BRS must be off by default")
	}
	drv.setBRS(true)
	if !drv.brs() {
		t.Fatal("SetBRS(true) did not enable BRS")
	}
	if !drv.config().BRS {
		t.Fatal("config().BRS not updated")
	}
	drv.setBRS(false)
	if drv.brs() {
		t.Fatal("SetBRS(false) did not disable BRS")
	}
}
