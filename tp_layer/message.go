package tp_layer

import (
	"encoding/hex"
	"fmt"
)

// CanMessage represents a standard 11-bit CAN or CAN FD data frame.
type CanMessage struct {
	ArbitrationID uint32
	Data          []byte
	IsFD          bool
}

// String 方法提供了 CanMessage 的字符串表示形式。
func (m *CanMessage) String() string {
	idStr := fmt.Sprintf("%03x", m.ArbitrationID)
	dataStr := hex.EncodeToString(m.Data)
	flagStr := ""
	if m.IsFD {
		flagStr = " (fd)"
	}
	return fmt.Sprintf("<CanMessage %s [%d]%s \"%s\">", idStr, len(m.Data), flagStr, dataStr)
}

// State 定义了收发状态机的状态。
type State uint8

const (
	StateIdle State = iota
	StateWaitFC
	StateWaitCF
	StateTransmit
)

// FlowStatus 定义了流控帧的状态。
type FlowStatus uint8

const (
	FlowStatusContinueToSend FlowStatus = 0x00
	FlowStatusWait           FlowStatus = 0x01
	FlowStatusOverflow       FlowStatus = 0x02
)
