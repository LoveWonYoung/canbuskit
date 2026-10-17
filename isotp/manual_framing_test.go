package isotp_test

import (
	"bytes"
	"testing"

	"github.com/LoveWonYoung/canbuskit/v2/isotp"
)

func TestExportedManualFrameEncoders(t *testing.T) {
	tests := []struct {
		name string
		make func() ([]byte, error)
		want []byte
	}{
		{
			name: "single frame",
			make: func() ([]byte, error) {
				return isotp.EncodeSingleFrame([]byte{0x22, 0xF1, 0x90}, false)
			},
			want: []byte{0x03, 0x22, 0xF1, 0x90},
		},
		{
			name: "first frame",
			make: func() ([]byte, error) {
				return isotp.EncodeFirstFrame([]byte{1, 2, 3, 4, 5, 6}, 20, false)
			},
			want: []byte{0x10, 0x14, 1, 2, 3, 4, 5, 6},
		},
		{
			name: "flow control frame",
			make: func() ([]byte, error) {
				return isotp.EncodeFlowControlFrame(isotp.FlowStatusContinueToSend, 8, 0xF5)
			},
			want: []byte{0x30, 0x08, 0xF5},
		},
		{
			name: "consecutive frame",
			make: func() ([]byte, error) {
				return isotp.EncodeConsecutiveFrame([]byte{7, 8, 9}, 1, false)
			},
			want: []byte{0x21, 7, 8, 9},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.make()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, tc.want) {
				t.Fatalf("encoded frame = % X, want % X", got, tc.want)
			}
		})
	}
}

func TestExportedManualFrameEncodersValidateInput(t *testing.T) {
	if _, err := isotp.EncodeSingleFrame(make([]byte, 8), false); err == nil {
		t.Fatal("classic CAN Single Frame accepted an oversized payload")
	}
	if _, err := isotp.EncodeFirstFrame([]byte{1, 2, 3}, 3, false); err == nil {
		t.Fatal("First Frame accepted a total size equal to its first chunk")
	}
	if _, err := isotp.EncodeConsecutiveFrame(make([]byte, 8), 1, false); err == nil {
		t.Fatal("classic CAN Consecutive Frame accepted an oversized chunk")
	}
	if _, err := isotp.EncodeFlowControlFrame(isotp.FlowStatus(3), 0, 0); err == nil {
		t.Fatal("Flow Control Frame accepted an invalid flow status")
	}
	if _, err := isotp.EncodeFlowControlFrame(isotp.FlowStatusContinueToSend, 0, 0x80); err == nil {
		t.Fatal("Flow Control Frame accepted a reserved STmin value")
	}
}
