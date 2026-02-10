package driver

import (
	"context"
	"log"
)

type DirectionType byte

const (
	TX DirectionType = iota
	RX
)

// dataLenToDlc 将CAN/CAN-FD的实际数据字节长度转换为DLC码
func dataLenToDlc(len int) byte {
	if len <= 8 {
		return byte(len)
	}
	switch {
	case len <= 12:
		return 9
	case len <= 16:
		return 10
	case len <= 20:
		return 11
	case len <= 24:
		return 12
	case len <= 32:
		return 13
	case len <= 48:
		return 14
	case len <= 64:
		return 15
	default:
		return 15
	}
}

// dlcToLen 将CAN/CAN-FD的DLC码转换为实际的数据字节长度
func dlcToLen(dlc byte) int {
	if dlc <= 8 {
		return int(dlc)
	}
	switch dlc {
	case 9:
		return 12
	case 10:
		return 16
	case 11:
		return 20
	case 12:
		return 24
	case 13:
		return 32
	case 14:
		return 48
	case 15:
		return 64
	default:
		return 64
	}
}

// logCANMessage 统一的CAN消息日志记录函数
func logCANMessage(direction string, id uint32, dlc byte, data []byte, canType CanType) {
	typeStr := "CANFD"
	if canType == CAN {
		typeStr = "CAN  "
	}
	format := "%s %s: ID=0x%03X, DLC=%02d, Data=% 02X"
	log.Printf(format, direction, typeStr, id, dlc, data)
}

// UnifiedCANMessage 是一个通用的CAN/CAN-FD消息结构体，用于在channel中传递,它屏蔽了底层 CAN_MSG 和 CANFD_MSG 的差异。
type UnifiedCANMessage struct {
	Direction DirectionType
	ID        uint32
	DLC       byte
	Data      [64]byte // 使用64字节以兼容CAN-FD
	IsFD      bool     // 标志位，用于区分是CAN还是CAN-FD消息
}

// CANDriver 定义了CAN/CAN-FD驱动的统一接口
type CANDriver interface {
	Init() error
	Start()
	Stop()
	Write(id int32, data []byte) error
	RxChan() <-chan UnifiedCANMessage
	Context() context.Context
}
