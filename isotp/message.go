package isotp

import (
	"encoding/hex"
	"fmt"
)

// linkFrame represents a standard 11-bit CAN or CAN FD data frame.
type linkFrame struct {
	ArbitrationID uint32
	Data          []byte
	FD            bool
}

// String 方法提供了 linkFrame 的字符串表示形式。
func (m *linkFrame) String() string {
	idStr := fmt.Sprintf("%03x", m.ArbitrationID)
	dataStr := hex.EncodeToString(m.Data)
	flagStr := ""
	if m.FD {
		flagStr = " (fd)"
	}
	return fmt.Sprintf("<linkFrame %s [%d]%s \"%s\">", idStr, len(m.Data), flagStr, dataStr)
}

// state 定义了收发状态机的状态。
type state uint8

const (
	stateIdle state = iota
	stateWaitFC
	stateWaitCF
	stateTransmit
)

// FlowStatus 定义了流控帧的状态。
type FlowStatus uint8

const (
	FlowStatusContinueToSend FlowStatus = 0x00
	FlowStatusWait           FlowStatus = 0x01
	FlowStatusOverflow       FlowStatus = 0x02
)
