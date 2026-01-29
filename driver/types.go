package driver

import "time"

const (
	CHANNEL1 = iota
	CHANNEL2
	CHANNEL3
	CHANNEL4
)

// Vector
const (
	CANOEVN1630 = 57
	CANOEVN1640 = 59
)

// 缓冲区和轮询配置常量
const (
	RxChannelBufferSize = 1024                  // 接收通道缓冲区大小
	MsgBufferSize       = 1024                  // 消息缓冲区大小
	PollingInterval     = time.Millisecond      // 轮询间隔
	InitDelay           = 20 * time.Millisecond // 初始化延迟
)

// CanType 定义 CAN 类型 (与 Windows 版本保持一致)
type CanType byte

const (
	CAN   CanType = 0
	CANFD CanType = 1
)
