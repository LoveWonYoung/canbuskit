package driver

import (
	"context"
	"errors"
	"log"
	"sync"
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
	if !printLogEnabled() {
		return
	}
	if filter := logFilter.Load(); filter != nil && !filter.allows(id) {
		return
	}
	typeStr := "CANFD"
	if canType == CAN {
		typeStr = "CAN  "
	}
	format := "%s %s: ID=0x%03X, DLC=%02d, Data=% 02X"
	log.Printf(format, direction, typeStr, id, dlc, data)
}

// CanFrame 是一个通用的CAN/CAN-FD消息结构体，用于在channel中传递,它屏蔽了底层 CAN_MSG 和 CANFD_MSG 的差异。
type CanFrame struct {
	Direction DirectionType
	ID        uint32
	DLC       byte
	Data      [64]byte // 使用64字节以兼容CAN-FD
	IsFD      bool     // 标志位，用于区分是CAN还是CAN-FD消息
	BRS       bool     // CAN-FD bit rate switch; ignored for classic CAN
}

// DataLength returns the payload length represented by DLC.
func (m CanFrame) DataLength() int {
	return dlcToLen(m.DLC)
}

// CANDriver 定义了CAN/CAN-FD驱动的统一接口
type CANDriver interface {
	Init() error
	Start()
	Stop()
	Write(id int32, fd bool, data []byte) error
	RxChan() <-chan CanFrame
	IsFDMode() bool
}
type CANFDInitConfig struct {
	NBT_BRP  byte
	NBT_SEG1 byte
	NBT_SEG2 byte
	NBT_SJW  byte
	DBT_BRP  byte
	DBT_SEG1 byte
	DBT_SEG2 byte
	DBT_SJW  byte
}

func (cfg CANFDInitConfig) ValidateTiming() error {
	if cfg.NBT_SEG1 == 0 || cfg.NBT_SEG2 == 0 || cfg.NBT_SJW == 0 {
		return errors.New("CAN FD nominal timing (NBT_SEG1/SEG2/SJW) must be non-zero")
	}
	if cfg.DBT_SEG1 == 0 || cfg.DBT_SEG2 == 0 || cfg.DBT_SJW == 0 {
		return errors.New("CAN FD data timing (DBT_SEG1/SEG2/SJW) must be non-zero")
	}
	return nil
}

// ValidateWithBRP checks timing segments and BRP (required by Toomoss/PCAN bitrate strings).
func (cfg CANFDInitConfig) ValidateWithBRP() error {
	if err := cfg.ValidateTiming(); err != nil {
		return err
	}
	if cfg.NBT_BRP == 0 || cfg.DBT_BRP == 0 {
		return errors.New("CAN FD BRP (NBT_BRP/DBT_BRP) must be non-zero")
	}
	return nil
}

var ErrDriverNotInitialized = errors.New("CAN driver is not initialized")

// ErrorStartingCANDriver is an optional extension for drivers that can report
// startup failures without changing the legacy CANDriver interface.
type ErrorStartingCANDriver interface {
	StartWithError() error
}

// BRSController is an optional extension that enables CAN-FD bit rate
// switching on transmitted FD frames. Default is off.
type BRSController interface {
	SetBRS(enabled bool)
	BRS() bool
}

// driverLifecycle serializes initialization/cleanup and makes the read loop
// start/stop sequence idempotent.
type driverLifecycle struct {
	opMu        sync.Mutex
	mu          sync.Mutex
	readWG      sync.WaitGroup
	initialized bool
	running     bool
}

func (l *driverLifecycle) isInitialized() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.initialized
}

func (l *driverLifecycle) markInitialized() {
	l.mu.Lock()
	l.initialized = true
	l.mu.Unlock()
}

func (l *driverLifecycle) start(readLoop func()) bool {
	l.mu.Lock()
	if !l.initialized || l.running {
		l.mu.Unlock()
		return false
	}
	l.running = true
	l.readWG.Add(1)
	l.mu.Unlock()

	go func() {
		defer func() {
			l.mu.Lock()
			l.running = false
			l.mu.Unlock()
			l.readWG.Done()
		}()
		readLoop()
	}()
	return true
}

// cancelAndWait marks the instance stopped, calls cancel, and waits until the
// hardware read loop is no longer executing vendor code.
func (l *driverLifecycle) cancelAndWait(cancel context.CancelFunc) bool {
	l.mu.Lock()
	wasInitialized := l.initialized
	l.initialized = false
	l.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	l.readWG.Wait()
	return wasInitialized
}
