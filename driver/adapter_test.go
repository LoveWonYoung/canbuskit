package driver

import "testing"

func TestAdapterDropsTXEchoAndNonStandardID(t *testing.T) {
	adapter := &Adapter{}

	if _, ok := adapter.convertRXMessage(UnifiedCANMessage{
		Direction: TX,
		ID:        0x123,
		DLC:       1,
	}, true); ok {
		t.Fatal("TX echo was passed to ISO-TP")
	}

	if _, ok := adapter.convertRXMessage(UnifiedCANMessage{
		Direction: RX,
		ID:        0x800,
		DLC:       1,
	}, true); ok {
		t.Fatal("non-standard CAN ID was passed to ISO-TP")
	}
}

func TestAdapterConvertsStandardRXFrame(t *testing.T) {
	adapter := &Adapter{}
	raw := UnifiedCANMessage{
		Direction: RX,
		ID:        0x7E8,
		DLC:       3,
		IsFD:      true,
	}
	copy(raw.Data[:], []byte{0x02, 0x50, 0x03})

	msg, ok := adapter.convertRXMessage(raw, true)
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
