//go:build windows

package driver

import (
	"context"
	"errors"
	"fmt"
	"log"
	"syscall"
	"time"
	"unsafe"
)

// 缓冲区和轮询配置常量
const (
	RxChannelBufferSize = 1024                  // 接收通道缓冲区大小
	MsgBufferSize       = 1024                  // 消息缓冲区大小
	PollingInterval     = time.Millisecond      // 轮询间隔
	InitDelay           = 20 * time.Millisecond // 初始化延迟
)

var (
	CANFDInit, _            = syscall.GetProcAddress(UsbDeviceDLL, "CANFD_Init")
	CANFDStartGetMsg, _     = syscall.GetProcAddress(UsbDeviceDLL, "CANFD_StartGetMsg")
	CANFD_GetMsg, _         = syscall.GetProcAddress(UsbDeviceDLL, "CANFD_GetMsg")
	CANFD_SendMsg, _        = syscall.GetProcAddress(UsbDeviceDLL, "CANFD_SendMsg")
	CANFD_GetCANSpeedArg, _ = syscall.GetProcAddress(UsbDeviceDLL, "CANFD_GetCANSpeedArg")
)

type CANFD_INIT_CONFIG struct {
	Mode         byte
	ISOCRCEnable byte
	RetrySend    byte
	ResEnable    byte
	NBT_BRP      byte
	NBT_SEG1     byte
	NBT_SEG2     byte
	NBT_SJW      byte
	DBT_BRP      byte
	DBT_SEG1     byte
	DBT_SEG2     byte
	DBT_SJW      byte
	__Res0       []byte
}

type CANFD_MSG struct {
	ID        uint32
	DLC       byte
	Flags     byte
	__Res0    byte
	__Res1    byte
	TimeStamp uint32
	Data      [64]byte
}

// dataLenToDlc 将CAN/CAN-FD的DLC码转换为实际的数据字节长度
func dataLenToDlc(dlc byte) int {
	if dlc <= 8 && dlc > 0 {
		return int(dlc)
	}
	switch {
	case dlc <= 12:
		return 9
	case dlc <= 16:
		return 10
	case dlc <= 20:
		return 11
	case dlc <= 24:
		return 12
	case dlc <= 32:
		return 13
	case dlc <= 48:
		return 14
	case dlc <= 64:
		return 15
	default:
		return 15 // 对于无效值，默认返回15
	}
}

const (
	CanChannel  = 0
	SpeedBpsNBT = 500_000
	SpeedBpsDBT = 200_0000
)

const (
	GET_FIRMWARE_INFO = 1
	CAN_MODE_LOOPBACK = 0
	CAN_SEND_MSG      = 1
	CAN_GET_MSG       = 1
	CAN_GET_STATUS    = 0
	CAN_SCH           = 0
	CAN_SUCCESS       = 0
	SendCANIndex      = 0
	ReadCANIndex      = 0
)

const (
	CAN_MSG_FLAG_STD   = 0
	CANFD_MSG_FLAG_BRS = 0x01 // CANFD加速帧标志
	CANFD_MSG_FLAG_ESI = 0x02 // CANFD错误状态指示
	CANFD_MSG_FLAG_FDF = 0x04 // CANFD帧标志
)

const (
	CAN CanType = iota
	CANFD
)

type CanType byte

type CanMix struct {
	rxChan  chan UnifiedCANMessage
	ctx     context.Context
	cancel  context.CancelFunc
	canType CanType
}

// logCANMessage 统一的CAN消息日志记录函数
func logCANMessage(direction string, id uint32, dlc byte, data []byte, canType CanType) {
	typeStr := "CANFD"
	if canType == CAN {
		typeStr = "CAN  "
	}
	format := "%s %s: ID=0x%03X, DLC=%02d, Data=% 02X"
	log.Printf(format, direction, typeStr, id, dataLenToDlc(dlc), data)
	// fmt.Printf(format+"\n", direction, typeStr, id, dataLenToDlc(dlc), data)
}

func NewCanMix(canType CanType) *CanMix {
	ctx, cancel := context.WithCancel(context.Background())
	return &CanMix{
		rxChan:  make(chan UnifiedCANMessage, RxChannelBufferSize),
		ctx:     ctx,
		cancel:  cancel,
		canType: canType,
	}
}

func (c *CanMix) Init() error {
	UsbScan()
	UsbOpen()
	var canFDInitConfig = CANFD_INIT_CONFIG{
		Mode:         0,
		RetrySend:    1,
		ISOCRCEnable: 1,
		ResEnable:    1,
		NBT_BRP:      1,
		NBT_SEG1:     59,
		NBT_SEG2:     20,
		NBT_SJW:      2,
		DBT_BRP:      1,
		DBT_SEG1:     14,
		DBT_SEG2:     5,
		DBT_SJW:      2,
	}
	fdSpeed, _, _ := syscall.SyscallN(CANFD_GetCANSpeedArg, uintptr(DevHandle[DEVIndex]), uintptr(unsafe.Pointer(&canFDInitConfig)), uintptr(SpeedBpsNBT), uintptr(SpeedBpsDBT))
	canfdInit, _, _ := syscall.SyscallN(CANFDInit, uintptr(DevHandle[DEVIndex]), uintptr(CanChannel), uintptr(unsafe.Pointer(&canFDInitConfig)))
	fdStart, _, _ := syscall.SyscallN(CANFDStartGetMsg, uintptr(DevHandle[DEVIndex]), uintptr(CanChannel))
	time.Sleep(InitDelay)
	if !(canfdInit == 0 && fdStart == 0 && fdSpeed == 0) {
		log.Println("错误: CAN硬件初始化失败！")
		return fmt.Errorf("错误: CAN硬件初始化失败！")
	}
	log.Println("CAN硬件初始化成功。")
	return nil
}

func (c *CanMix) Start() {
	log.Println("CAN-FD驱动的中央读取服务已启动...")
	// 在启动轮询前先尝试清空设备内已存在的报文，避免初始化时大量旧报文填满接收缓冲
	c.drainInitialBuffer()
	log.Println("--------------------------------------------------------Clear Cache-----------------------------------------------------")
	go c.readLoop()
}

func (c *CanMix) Stop() {
	log.Println("正在停止CAN-FD驱动的读取服务...")
	c.cancel()
	UsbClose()
}

func (c *CanMix) readLoop() {
	ticker := time.NewTicker(PollingInterval)
	defer ticker.Stop()
	var canFDMsg [MsgBufferSize]CANFD_MSG
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			getCanFDMsgNum, _, _ := syscall.SyscallN(
				CANFD_GetMsg,
				uintptr(DevHandle[DEVIndex]),
				uintptr(CanChannel),
				uintptr(unsafe.Pointer(&canFDMsg[0])),
				uintptr(len(canFDMsg)),
			)

			if getCanFDMsgNum <= 0 {
				continue
			}

			for i := 0; i < int(getCanFDMsgNum); i++ {
				msg := canFDMsg[i]
				actualLen := dataLenToDlc(msg.DLC)
				if actualLen == 0 {
					continue
				}
				unifiedMsg := UnifiedCANMessage{
					ID: msg.ID, DLC: msg.DLC, Data: msg.Data, IsFD: msg.Flags == CANFD_MSG_FLAG_FDF,
				}

				// 使用统一的日志函数
				msgType := c.canType
				if msg.Flags == CAN_MSG_FLAG_STD {
					msgType = CAN
				} else {
					msgType = CANFD
				}
				logCANMessage("RX", unifiedMsg.ID, unifiedMsg.DLC, unifiedMsg.Data[:unifiedMsg.DLC], msgType)

				select {
				case c.rxChan <- unifiedMsg:
				default:
					log.Println("警告: 驱动接收channel(FD)已满，消息被丢弃")
				}
			}
		}
	}
}

// drainInitialBuffer 从设备反复读取直到没有剩余消息，目的是在驱动正式进入
// readLoop 前清空硬件缓存，避免初始化时把历史报文一次性塞满 rxChan
func (c *CanMix) drainInitialBuffer() {
	var canFDMsg [MsgBufferSize]CANFD_MSG
	for {
		n, _, _ := syscall.SyscallN(
			CANFD_GetMsg,
			uintptr(DevHandle[DEVIndex]),
			uintptr(CanChannel),
			uintptr(unsafe.Pointer(&canFDMsg[0])),
			uintptr(len(canFDMsg)),
		)
		if int(n) <= 0 {
			break
		}
		// 若需要可在此统计已丢弃数量，当前选择静默丢弃以减少启动日志噪声
	}
}

func (c *CanMix) Write(id int32, data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("数据长度 %d ", len(data))
	} else if len(data) > 64 && c.canType == CANFD {
		return fmt.Errorf("数据长度 %d 超过CAN-FD最大长度64", len(data))
	} else if len(data) >= 8 && c.canType == CAN {
		return fmt.Errorf("数据长度 %d 超过CAN最大长度8", len(data))
	}

	var canFDMsg [1]CANFD_MSG
	var tempData [64]byte
	copy(tempData[:], data)
	canFDMsg[0].ID = uint32(id)
	switch c.canType {
	case CAN:
		canFDMsg[0].Flags = 0
	case CANFD:
		canFDMsg[0].Flags = CANFD_MSG_FLAG_FDF
	default:
		canFDMsg[0].Flags = CANFD_MSG_FLAG_FDF
	}

	canFDMsg[0].DLC = byte(len(data))
	if byte(len(data)) < 8 {
		canFDMsg[0].DLC = 8
	}
	canFDMsg[0].Data = tempData
	sendRet, _, _ := syscall.SyscallN(CANFD_SendMsg, uintptr(DevHandle[DEVIndex]), uintptr(CanChannel), uintptr(unsafe.Pointer(&canFDMsg[0])), uintptr(len(canFDMsg)))

	if int(sendRet) == len(canFDMsg) {
		logCANMessage("TX", uint32(id), byte(len(data)), canFDMsg[0].Data[:canFDMsg[0].DLC], c.canType)
	} else {
		log.Printf("错误: CAN/CANFD消息发送失败, ID=0x%03X", id)
		return errors.New("CAN/CANFD消息发送失败")
	}
	return nil
}

func (c *CanMix) RxChan() <-chan UnifiedCANMessage { return c.rxChan }

func (c *CanMix) Context() context.Context { return c.ctx }
