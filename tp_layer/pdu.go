package tp_layer

import (
	"encoding/binary"
	"fmt"
	"time"
)

type ISOTPFrame interface{}
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

func ParseFrame(msg *CanMessage, rxPrefixSize int) (ISOTPFrame, error) {
	if len(msg.Data) <= rxPrefixSize {
		return nil, fmt.Errorf("CAN数据长度 (%d) 小于等于前缀长度 (%d)", len(msg.Data), rxPrefixSize)
	}

	payload := msg.Data[rxPrefixSize:]
	pciType := payload[0] & 0xF0

	switch pciType {
	case 0x00: // Single Frame
		length := int(payload[0] & 0x0F)
		var data []byte
		if length == 0 { // Escaped length for CAN FD
			if len(payload) < 2 {
				return nil, fmt.Errorf("SF(FD)长度不足2字节")
			}
			length = int(payload[1])
			if len(payload)-2 < length {
				return nil, fmt.Errorf("SF(FD)数据不完整")
			}
			data = payload[2 : 2+length]
		} else { // Standard SF
			if len(payload)-1 < length {
				return nil, fmt.Errorf("SF数据不完整")
			}
			data = payload[1 : 1+length]
		}
		return &SingleFrame{Data: data}, nil
	case 0x10: // First Frame
		if len(payload) < 2 {
			return nil, fmt.Errorf("FF长度不足2字节")
		}
		totalSize := (int(payload[0]&0x0F) << 8) | int(payload[1])
		dataStart := 2
		if totalSize == 0 { // 32-bit length
			if len(payload) < 6 {
				return nil, fmt.Errorf("FF(long)长度不足6字节")
			}
			totalSize = int(binary.BigEndian.Uint32(payload[2:6]))
			dataStart = 6
		}
		return &FirstFrame{TotalSize: totalSize, Data: payload[dataStart:]}, nil
	case 0x20: // Consecutive Frame
		return &ConsecutiveFrame{SequenceNumber: int(payload[0] & 0x0F), Data: payload[1:]}, nil
	case 0x30: // Flow Control Frame
		if len(payload) < 3 {
			return nil, fmt.Errorf("FC长度不足3字节")
		}
		return &FlowControlFrame{
			FlowStatus: FlowStatus(payload[0] & 0x0F),
			BlockSize:  int(payload[1]),
			STmin:      decodeSTmin(payload[2]),
		}, nil
	}
	return nil, fmt.Errorf("未知PCI类型: 0x%02X", pciType)
}
