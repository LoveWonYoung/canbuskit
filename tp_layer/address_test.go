package tp_layer

import "testing"

func TestNewAddressAcceptsStandardCANIDs(t *testing.T) {
	addr, err := NewAddress(0x000, 0x7FF)
	if err != nil {
		t.Fatalf("NewAddress() error = %v", err)
	}
	if addr.TxID != 0x000 || addr.RxID != 0x7FF {
		t.Fatalf("NewAddress() = %+v", addr)
	}
}

func TestNewAddressRejectsExtendedCANIDs(t *testing.T) {
	tests := []struct {
		txID uint32
		rxID uint32
	}{
		{txID: 0x800, rxID: 0x7FF},
		{txID: 0x7FF, rxID: 0x800},
	}

	for _, tc := range tests {
		if _, err := NewAddress(tc.txID, tc.rxID); err == nil {
			t.Fatalf("NewAddress(0x%X, 0x%X) unexpectedly succeeded", tc.txID, tc.rxID)
		}
	}
}

func TestAddressValidateRejectsNil(t *testing.T) {
	var addr *Address
	if err := addr.Validate(); err == nil {
		t.Fatal("nil address unexpectedly validated")
	}
}

func TestAddressIsForMeUsesReceiveID(t *testing.T) {
	addr := &Address{TxID: 0x700, RxID: 0x708}
	if !addr.IsForMe(&CanMessage{ArbitrationID: 0x708}) {
		t.Fatal("matching receive ID was rejected")
	}
	if addr.IsForMe(&CanMessage{ArbitrationID: 0x700}) {
		t.Fatal("transmit ID was accepted as receive ID")
	}
}
