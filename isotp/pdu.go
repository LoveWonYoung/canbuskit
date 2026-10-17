package isotp

import (
	"encoding/binary"
	"fmt"
	"time"
)

type PDU interface{ isPDU() }
type SingleFrame struct{ Data []byte }
type FirstFrame struct {
	TotalSize int
	Data      []byte
}
type ConsecutiveFrame struct {
	SequenceNumber int
	Data           []byte
}
type FlowControlFrame struct {
	FlowStatus FlowStatus
	BlockSize  int
	STmin      time.Duration
}

func (*SingleFrame) isPDU()      {}
func (*FirstFrame) isPDU()       {}
func (*ConsecutiveFrame) isPDU() {}
func (*FlowControlFrame) isPDU() {}

func decodeSTmin(stMinByte byte) time.Duration {
	if stMinByte <= 0x7F {
		return time.Duration(stMinByte) * time.Millisecond
	}
	if stMinByte >= 0xF1 && stMinByte <= 0xF9 {
		return time.Duration(stMinByte-0xF0) * 100 * time.Microsecond
	}
	// Per standard, other values are reserved and should be interpreted as the max (127ms)
	return 127 * time.Millisecond
}

func ParseFrame(payload []byte) (PDU, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("CAN payload is empty")
	}
	pciType := payload[0] & 0xF0

	switch pciType {
	case 0x00: // Single Frame
		length := int(payload[0] & 0x0F)
		var data []byte
		if length == 0 { // Escaped length for CAN FD
			if len(payload) == 1 {
				return &SingleFrame{Data: []byte{}}, nil
			}
			if len(payload) < 2 {
				return nil, fmt.Errorf("escaped single frame is shorter than 2 bytes")
			}
			length = int(payload[1])
			if len(payload)-2 < length {
				return nil, fmt.Errorf("escaped single-frame payload is incomplete")
			}
			data = payload[2 : 2+length]
		} else { // Standard SF
			if len(payload)-1 < length {
				return nil, fmt.Errorf("single-frame payload is incomplete")
			}
			data = payload[1 : 1+length]
		}
		return &SingleFrame{Data: data}, nil
	case 0x10: // First Frame
		if len(payload) < 2 {
			return nil, fmt.Errorf("first frame is shorter than 2 bytes")
		}
		totalSize := (int(payload[0]&0x0F) << 8) | int(payload[1])
		dataStart := 2
		if totalSize == 0 { // 32-bit length
			if len(payload) < 6 {
				return nil, fmt.Errorf("long first frame is shorter than 6 bytes")
			}
			totalSize = int(binary.BigEndian.Uint32(payload[2:6]))
			dataStart = 6
			if totalSize <= 4095 {
				return nil, fmt.Errorf("long first-frame length %d must use the 12-bit format", totalSize)
			}
		}
		if totalSize <= len(payload)-dataStart {
			return nil, fmt.Errorf("first-frame total length %d does not exceed its data length %d", totalSize, len(payload)-dataStart)
		}
		return &FirstFrame{TotalSize: totalSize, Data: payload[dataStart:]}, nil
	case 0x20: // Consecutive Frame
		return &ConsecutiveFrame{SequenceNumber: int(payload[0] & 0x0F), Data: payload[1:]}, nil
	case 0x30: // Flow Control Frame
		if len(payload) < 3 {
			return nil, fmt.Errorf("flow-control frame is shorter than 3 bytes")
		}
		status := FlowStatus(payload[0] & 0x0F)
		if status > FlowStatusOverflow {
			return nil, fmt.Errorf("invalid flow-control status: 0x%X", status)
		}
		return &FlowControlFrame{
			FlowStatus: status,
			BlockSize:  int(payload[1]),
			STmin:      decodeSTmin(payload[2]),
		}, nil
	}
	return nil, fmt.Errorf("unknown protocol control information type: 0x%02X", pciType)
}
