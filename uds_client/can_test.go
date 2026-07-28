package uds_client

import (
	"testing"

	"github.com/LoveWonYoung/canbuskit/driver"
)

func TestConvertRXMessageDropsTXEchoAndNonStandardID(t *testing.T) {
	if _, ok := convertRXMessage(driver.CanFrame{
		Direction: driver.TX,
		ID:        0x123,
		DLC:       1,
	}); ok {
		t.Fatal("TX echo was passed to ISO-TP")
	}

	if _, ok := convertRXMessage(driver.CanFrame{
		Direction: driver.RX,
		ID:        0x800,
		DLC:       1,
	}); ok {
		t.Fatal("non-standard CAN ID was passed to ISO-TP")
	}
}

func TestConvertRXMessageConvertsStandardFrame(t *testing.T) {
	raw := driver.CanFrame{
		Direction: driver.RX,
		ID:        0x7E8,
		DLC:       3,
		IsFD:      true,
	}
	copy(raw.Data[:], []byte{0x02, 0x50, 0x03})

	msg, ok := convertRXMessage(raw)
	if !ok {
		t.Fatal("standard RX frame was dropped")
	}
	if msg.ArbitrationID != raw.ID || !msg.IsFD {
		t.Fatalf("unexpected converted frame: %+v", msg)
	}
	if len(msg.Data) != 3 || msg.Data[0] != 0x02 || msg.Data[2] != 0x03 {
		t.Fatalf("unexpected converted data: % X", msg.Data)
	}
}
