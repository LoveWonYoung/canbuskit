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

var (
	CAN_GetCANSpeedArg, _ = syscall.GetProcAddress(UsbDeviceDLL, "CAN_GetCANSpeedArg")
	CAN_Init, _           = syscall.GetProcAddress(UsbDeviceDLL, "CAN_Init")
	CAN_SendMsg, _        = syscall.GetProcAddress(UsbDeviceDLL, "CAN_SendMsg")
	CAN_GetMsg, _         = syscall.GetProcAddress(UsbDeviceDLL, "CAN_GetMsg")
	CAN_StartGetMsg, _    = syscall.GetProcAddress(UsbDeviceDLL, "CAN_StartGetMsg")
)

type CAN_MSG struct {
	ID int32

	TimeStamp int32

	RemoteFlag byte

	ExternFlag byte

	DataLen byte

	Data [8]byte

	TimeStampHigh byte
}

type CAN_FILTER_CONFIG struct {
	Enable byte //使能该过滤器，1-使能，0-禁止

	FilterIndex byte //过滤器索引号，取值范围为0到13

	FilterMode byte //过滤器模式，0-屏蔽位模式，1-标识符列表模式

	ExtFrame byte //过滤的帧类型标志，为1 代表要过滤的为扩展帧，为0 代表要过滤的为标准帧。

	ID_Std_Ext int32 //验收码ID

	ID_IDE int32 //验收码IDE

	ID_RTR int32 //验收码RTR

	MASK_Std_Ext int32 //屏蔽码ID，该项只有在过滤器模式为屏蔽位模式时有用

	MASK_IDE int32 //屏蔽码IDE，该项只有在过滤器模式为屏蔽位模式时有用

	MASK_RTR int32 //屏蔽码RTR，该项只有在过滤器模式为屏蔽位模式时有用

}

type CAN_INIT_CONFIG struct {
	CAN_BRP uint

	CAN_SJW byte

	CAN_BS1 byte

	CAN_BS2 byte

	CAN_Mode byte

	CAN_ABOM byte

	CAN_NART byte

	CAN_RFLM byte

	CAN_TXFP byte
}

// ToomossCan 结构体现在与 ToomossCanfd 非常相似

type ToomossCan struct {
	rxChan chan UnifiedCANMessage // [修正]

	ctx context.Context

	cancel context.CancelFunc
}

// 确保 ToomossCan 实现了 CANDriver 接口

var _ CANDriver = (*ToomossCan)(nil)

// NewToomossCan 是新的构造函数

func NewToomossCan() *ToomossCan {

	ctx, cancel := context.WithCancel(context.Background())

	return &ToomossCan{

		rxChan: make(chan UnifiedCANMessage, 1024), // [修正]

		ctx: ctx,

		cancel: cancel,
	}

}

// Init 方法重构，返回 error

func (tSelf *ToomossCan) Init() error {

	UsbScan()

	UsbOpen()

	var canInitConfig = CAN_INIT_CONFIG{
		CAN_Mode: 0,
		CAN_ABOM: 0,
		CAN_NART: 1,
		CAN_RFLM: 0,
		CAN_TXFP: 1,
		CAN_BRP:  4,
		CAN_BS1:  15,
		CAN_BS2:  5,
		CAN_SJW:  2,
	}

	r, _, _ := syscall.SyscallN(CAN_GetCANSpeedArg, uintptr(DevHandle[DEVIndex]), uintptr(unsafe.Pointer(&canInitConfig)), uintptr(500000))

	r1, _, _ := syscall.SyscallN(CAN_Init, uintptr(DevHandle[DEVIndex]), uintptr(CanChannel), uintptr(unsafe.Pointer(&canInitConfig)))

	canStart, _, _ := syscall.SyscallN(CAN_StartGetMsg, uintptr(DevHandle[DEVIndex]), uintptr(CanChannel))

	if !(r1 == CAN_SUCCESS && canStart == CAN_SUCCESS && r == CAN_SUCCESS) {

		return fmt.Errorf("标准CAN硬件初始化失败: CAN_Init=%d, CAN_StartGetMsg=%d", r1, canStart)

	}

	log.Println("标准CAN硬件初始化成功。")

	return nil

}

// Start 方法，启动后台读取goroutine

func (tSelf *ToomossCan) Start() {

	log.Println("标准CAN驱动的中央读取服务已启动...")

	go tSelf.readLoop()

}

// Stop 方法，停止服务并释放资源

func (tSelf *ToomossCan) Stop() {

	log.Println("正在停止标准CAN驱动...")

	tSelf.cancel()

	UsbClose()

}

// readLoop 是全新的后台读取和分发逻辑

func (tSelf *ToomossCan) readLoop() {

	ticker := time.NewTicker(3 * time.Millisecond)

	defer ticker.Stop()

	var canMsgBuffer [1024]CAN_MSG

	for {

		select {

		case <-tSelf.ctx.Done():

			log.Println("标准CAN中央读取服务已优雅退出。")

			return

		case <-ticker.C:

			canNum, _, _ := syscall.SyscallN(CAN_GetMsg, uintptr(DevHandle[DEVIndex]), uintptr(ReadCANIndex), uintptr(unsafe.Pointer(&canMsgBuffer[0])))
			if canNum <= 0 {

				continue

			}

			for i := 0; i < int(canNum); i++ {

				msg := canMsgBuffer[i]
				//fmt.Printf("MsgID = %X data % 02X DataLen %d\n", msg.ID, msg.Data, msg.DataLen)
				if msg.DataLen == 0 {

					continue

				}

				// 将硬件特定的 CAN_MSG 转换为我们统一的 UnifiedCANMessage

				unifiedMsg := UnifiedCANMessage{

					ID: uint32(msg.ID),

					DLC: msg.DataLen,

					IsFD: false, // 明确这是标准CAN帧

				}

				copy(unifiedMsg.Data[:], msg.Data[:])
				//    689.627181 1  3A1       Rx   d 8 27 0E 10 66 65 51 50 00
				log.Printf("1  %03X       Rx   d %d % 02X", unifiedMsg.ID, dataLenToDlc(unifiedMsg.DLC), unifiedMsg.Data[:unifiedMsg.DLC])
				//log.Printf("RX CAN: ID=0x%03X, DLC=%02d, Data=% 02X", unifiedMsg.ID, dataLenToDlc(unifiedMsg.DLC), unifiedMsg.Data[:unifiedMsg.DLC])

				// 根据ID分发统一的消息

				select {

				case tSelf.rxChan <- unifiedMsg:

				default:

					log.Println("警告: UDS响应channel(CAN)已满，消息被丢弃")

				}

			}

		}

	}

}

func (tSelf *ToomossCan) Write(id int32, data []byte) error {
	if len(data) > 8 {
		return fmt.Errorf("数据长度 %d 超过标准CAN最大长度8", len(data))
	}

	var canMsg CAN_MSG
	var canData [8]byte

	// 1. 将传入的数据准确地拷贝到 canData 数组中
	copy(canData[:], data)

	canMsg.ExternFlag = 0 // 假设我们总是发送标准帧
	canMsg.RemoteFlag = 0 // 假设我们不发送远程帧
	canMsg.ID = id
	canMsg.DataLen = 8    // <-- 关键修正：DLC 等于实际数据长度
	canMsg.Data = canData // <-- 使用只包含有效数据的数组

	canMsgs := []CAN_MSG{canMsg}

	// 使用 syscall 发送报文
	r, _, _ := syscall.SyscallN(CAN_SendMsg, uintptr(DevHandle[DEVIndex]), uintptr(CanChannel), uintptr(unsafe.Pointer(&canMsgs[0])), uintptr(len(canMsgs)))

	if int(r) == len(canMsgs) {
		// 日志打印实际发送的数据，更利于调试
		log.Printf("1  %03X       Tx   d %d % 02X", id, canMsg.DataLen, canMsg.Data)
		//log.Printf("TX CAN: ID=0x%03X, DLC=%02d, Data=% X", id, canMsg.DataLen, canMsg.Data)
		return nil
	}

	log.Printf("错误: 标准CAN消息发送失败, ID=0x%03X", id)
	return errors.New("标准CAN消息发送失败")
}

// UdsChan [重构] 为符合接口，添加这些方法

func (tSelf *ToomossCan) RxChan() <-chan UnifiedCANMessage { return tSelf.rxChan }

func (tSelf *ToomossCan) Context() context.Context { return tSelf.ctx }
