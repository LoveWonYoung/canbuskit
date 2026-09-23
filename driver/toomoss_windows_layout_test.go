//go:build windows

package driver

import (
	"testing"
	"unsafe"
)

func TestToomossMessageLayoutsMatchVendorHeaders(t *testing.T) {
	if got := unsafe.Sizeof(CAN_MSG{}); got != 20 {
		t.Fatalf("CAN_MSG size = %d, want 20", got)
	}
	if got := unsafe.Sizeof(CANFD_MSG{}); got != 76 {
		t.Fatalf("CANFD_MSG size = %d, want 76", got)
	}
	if got := unsafe.Offsetof(CAN_MSG{}.TimeStampHigh); got != 19 {
		t.Fatalf("CAN_MSG.TimeStampHigh offset = %d, want 19", got)
	}
	if got := unsafe.Offsetof(CANFD_MSG{}.TimeStamp); got != 8 {
		t.Fatalf("CANFD_MSG.TimeStamp offset = %d, want 8", got)
	}
}
