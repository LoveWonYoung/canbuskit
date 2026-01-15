package driver

import (
	"context"
)

// UnifiedCANMessage 是一个通用的CAN/CAN-FD消息结构体，用于在channel中传递。
// 它屏蔽了底层 CAN_MSG 和 CANFD_MSG 的差异。
type UnifiedCANMessage struct {
	ID   uint32
	DLC  byte
	Data [64]byte // 使用64字节以兼容CAN-FD
	IsFD bool     // 标志位，用于区分是CAN还是CAN-FD消息
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
