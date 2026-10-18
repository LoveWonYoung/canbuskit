//go:build windows

package backend

import (
	"github.com/LoveWonYoung/canbuskit/v2/can"
	"testing"
)

func TestApplyToomossTimingKeepsDefaults(t *testing.T) {
	cfg := can.Config{
		NominalTiming: can.BitTiming{BRP: 2, SEG1: 10, SEG2: 3, SJW: 1},
		DataTiming:    can.BitTiming{BRP: 1, SEG1: 7, SEG2: 2, SJW: 1},
	}
	got := applyToomossTiming(cfg)
	if got.Mode != 0 || got.RetrySend != 1 || got.ISOCRCEnable != 1 || got.ResEnable != 1 {
		t.Fatalf("applyToomossTiming() lost Toomoss defaults: %+v", got)
	}
	if got.NBT_BRP != 2 || got.NBT_SEG1 != 10 || got.DBT_SEG1 != 7 {
		t.Fatalf("applyToomossTiming() timing not applied: %+v", got)
	}
}

func TestToomossSetBRS(t *testing.T) {
	drv := newToomoss(can.DefaultConfig(can.FD, can.Channel1))
	if drv.brs() {
		t.Fatal("BRS must be off by default")
	}
	drv.setBRS(true)
	if !drv.brs() || !drv.config().BRS {
		t.Fatal("setBRS(true) did not enable BRS")
	}
}

func TestToomossClassicFlags(t *testing.T) {
	remoteFlag, externFlag := encodeToomossClassicFlags(can.Channel2, false, false)
	channel, remote, extended, errorFrame, txEcho := decodeToomossClassicFlags(remoteFlag, externFlag)
	if channel != can.Channel2 || remote || extended || errorFrame || txEcho {
		t.Fatalf(
			"decoded flags = channel %d, remote %t, extended %t, error %t, tx %t",
			channel, remote, extended, errorFrame, txEcho,
		)
	}

	channel, remote, extended, errorFrame, txEcho = decodeToomossClassicFlags(
		toomossClassicFlagTx|toomossClassicFlagChannel,
		0,
	)
	if channel != 3 || remote || extended || errorFrame || !txEcho {
		t.Fatalf(
			"decoded TX flags = channel %d, remote %t, extended %t, error %t, tx %t",
			channel, remote, extended, errorFrame, txEcho,
		)
	}
}

func TestToomossCANFDFlags(t *testing.T) {
	flags := byte(CANFD_MSG_FLAG_FDF | CANFD_MSG_FLAG_BRS | toomossCANFDFlagChannel | toomossCANFDFlagTx)
	channel, isFD, brs, txEcho := decodeToomossCANFDFlags(flags)
	if channel != 3 || !isFD || !brs || !txEcho {
		t.Fatalf("decoded flags = channel %d, FD %t, BRS %t, TX %t", channel, isFD, brs, txEcho)
	}

	_, isFD, brs, txEcho = decodeToomossCANFDFlags(0)
	if isFD || brs || txEcho {
		t.Fatalf("classic RX flags decoded as FD=%t BRS=%t TX=%t", isFD, brs, txEcho)
	}
}

func TestToomossCANFDIDFlags(t *testing.T) {
	id, remote, extended := decodeToomossCANFDID(0x123 | toomossCANFDIDFlagRemote | toomossCANFDIDFlagExtended)
	if id != 0x123 || !remote || !extended {
		t.Fatalf("decoded ID = 0x%X, remote %t, extended %t", id, remote, extended)
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

func TestToomossTimestampUS(t *testing.T) {
	ticks := (uint64(2) << 32) | 7
	if got := toomossTimestampUS(2, 7, 10); got != ticks*10 {
		t.Fatalf("CAN-FD timestamp = %d, want %d", got, ticks*10)
	}
	if got := toomossTimestampUS(2, 7, 100); got != ticks*100 {
		t.Fatalf("classic CAN timestamp = %d, want %d", got, ticks*100)
	}
}
