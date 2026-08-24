//go:build windows || (darwin && cgo)

package driver

import "testing"

func TestBuildCANFDInitConfig(t *testing.T) {
	timing := CANFDInitConfig{
		NBT_BRP: 2, NBT_SEG1: 10, NBT_SEG2: 3, NBT_SJW: 1,
		DBT_BRP: 1, DBT_SEG1: 7, DBT_SEG2: 2, DBT_SJW: 1,
	}
	cfg := BuildCANFDInitConfig(timing)
	if cfg.Mode != 0 || cfg.RetrySend != 1 || cfg.ISOCRCEnable != 1 || cfg.ResEnable != 1 {
		t.Fatalf("BuildCANFDInitConfig() lost Toomoss defaults: %+v", cfg)
	}
	if cfg.NBT_BRP != 2 || cfg.NBT_SEG1 != 10 || cfg.DBT_SEG1 != 7 {
		t.Fatalf("BuildCANFDInitConfig() timing not applied: %+v", cfg)
	}
}

func TestToomossSetCANFDTiming(t *testing.T) {
	drv := NewToomoss(CANFD, CHANNEL1)
	before := drv.canFDInitConfig
	timing := CANFDInitConfig{
		NBT_BRP: 3, NBT_SEG1: 11, NBT_SEG2: 4, NBT_SJW: 2,
		DBT_BRP: 2, DBT_SEG1: 8, DBT_SEG2: 3, DBT_SJW: 1,
	}
	drv.SetCANFDTiming(timing)
	got := drv.canFDInitConfig
	if got.Mode != before.Mode || got.RetrySend != before.RetrySend {
		t.Fatalf("SetCANFDTiming() changed non-timing fields: before=%+v after=%+v", before, got)
	}
	if got.NBT_BRP != 3 || got.DBT_SEG1 != 8 || got.DBT_SJW != 1 {
		t.Fatalf("SetCANFDTiming() timing not applied: %+v", got)
	}
}

func TestToomossClassicFlags(t *testing.T) {
	remoteFlag, externFlag := encodeToomossClassicFlags(CHANNEL2, false, false)
	channel, remote, extended, errorFrame, txEcho := decodeToomossClassicFlags(remoteFlag, externFlag)
	if channel != CHANNEL2 || remote || extended || errorFrame || txEcho {
		t.Fatalf(
			"decoded flags = channel %d, remote %t, extended %t, error %t, tx %t",
			channel, remote, extended, errorFrame, txEcho,
		)
	}
}

func TestToomossDLCToDataLen(t *testing.T) {
	if got := toomossDLCToDataLen(64, false); got != 8 {
		t.Fatalf("classic length = %d, want 8", got)
	}
	if got := toomossDLCToDataLen(64, true); got != 64 {
		t.Fatalf("CAN-FD length = %d, want 64", got)
	}
}
