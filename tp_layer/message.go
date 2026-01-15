package tp_layer

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// CanMessage 代表一个 CAN 报文 (ISO-11898)。
type CanMessage struct {
	ArbitrationID uint32
	Data          []byte
	IsExtendedID  bool
	IsFD          bool
	BitrateSwitch bool
}

// String 方法提供了 CanMessage 的字符串表示形式。
func (m *CanMessage) String() string {
	var idStr string
	if m.IsExtendedID {
		idStr = fmt.Sprintf("%08x", m.ArbitrationID)
	} else {
		idStr = fmt.Sprintf("%03x", m.ArbitrationID)
	}
	dataStr := hex.EncodeToString(m.Data)
	var flags []string
	if m.IsFD {
		flags = append(flags, "fd")
	}
	if m.BitrateSwitch {
		flags = append(flags, "bs")
	}
	var flagStr string
	if len(flags) > 0 {
		flagStr = fmt.Sprintf(" (%s)", strings.Join(flags, ","))
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
