//go:build windows

package driver

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// ControlCAN device types (CANalyst-II / USBCAN series).
const (
	VCI_USBCAN1  = 3
	VCI_USBCAN2  = 4
	VCI_USBCAN2A = 4

	VCI_USBCAN_E_U  = 20
	VCI_USBCAN_2E_U = 21

	STATUS_OK  = 1
	STATUS_ERR = 0

	canalystDLLName = "ControlCAN.dll"
)

// Btr holds SJA1000 BTR0/BTR1 timing register values.
type Btr struct {
	Btr0 byte
	Btr1 byte
}

var (
	Kbps_20    = Btr{0x18, 0x1C}
	Kbps_40    = Btr{0x87, 0xFF}
	Kbps_50    = Btr{0x09, 0x1C}
	Kbps_80    = Btr{0x83, 0xFF}
	Kbps_100   = Btr{0x04, 0x1C}
	Kbps_125   = Btr{0x03, 0x1C}
	Kbps_200   = Btr{0x81, 0xFA}
	Kbps_250   = Btr{0x01, 0x1C}
	Kbps_400   = Btr{0x80, 0xFA}
	Kbps_500   = Btr{0x00, 0x1C}
	Kbps_666   = Btr{0x80, 0xB6}
	Kbps_800   = Btr{0x00, 0x16}
	Kbps_1000  = Btr{0x00, 0x14}
	Kbps_33_33 = Btr{0x09, 0x6F}
	Kbps_66_66 = Btr{0x04, 0x6F}
	Kbps_83_33 = Btr{0x03, 0x6F}
)

var canalystBitrateTable = map[uint32]Btr{
	20_000:    Kbps_20,
	33_333:    Kbps_33_33,
	40_000:    Kbps_40,
	50_000:    Kbps_50,
	66_666:    Kbps_66_66,
	80_000:    Kbps_80,
	83_333:    Kbps_83_33,
	100_000:   Kbps_100,
	125_000:   Kbps_125,
	200_000:   Kbps_200,
	250_000:   Kbps_250,
	400_000:   Kbps_400,
	500_000:   Kbps_500,
	666_000:   Kbps_666,
	800_000:   Kbps_800,
	1_000_000: Kbps_1000,
}

// VCI_INIT_CONFIG matches ControlCAN.h.
type VCI_INIT_CONFIG struct {
	AccCode  uint32
	AccMask  uint32
	Reserved uint32
	Filter   uint8
	Timing0  uint8
	Timing1  uint8
	Mode     uint8
}

// VCI_CAN_OBJ matches ControlCAN.h.
type VCI_CAN_OBJ struct {
	ID         uint32
	TimeStamp  uint32
	TimeFlag   uint8
	SendType   uint8
	RemoteFlag uint8
	ExternFlag uint8
	DataLen    uint8
	Data       [8]uint8
	Reserved   [3]uint8
}

// CanalystII drives Zhou Ligong / Chuangxin CANalyst-II style adapters via ControlCAN.dll.
// These devices support classic (standard) CAN only.
type CanalystII struct {
	driverObservability
	CANChannel byte
	DeviceType int
	DeviceInd  int
	lifecycle  driverLifecycle
	fanout     *rxFanout
	cfg        Config
	ctx        context.Context
	cancel     context.CancelFunc
	rxChan     chan CanFrame
	opened     bool

	dll             *syscall.LazyDLL
	openDeviceProc  *syscall.LazyProc
	closeDeviceProc *syscall.LazyProc
	initCANProc     *syscall.LazyProc
	startCANProc    *syscall.LazyProc
	clearBufferProc *syscall.LazyProc
	transmitProc    *syscall.LazyProc
	receiveProc     *syscall.LazyProc
}

func NewCanalystII(canChannel byte) *CanalystII {
	return NewCanalystIIWithConfig(DefaultConfig(CAN, canChannel))
}

func NewCanalystIIWithConfig(cfg Config) *CanalystII {
	return NewCanalystIIWithDevice(cfg, VCI_USBCAN2, 0)
}

func NewCanalystIIWithDevice(cfg Config, deviceType, deviceInd int) *CanalystII {
	ctx, cancel := context.WithCancel(context.Background())
	return &CanalystII{
		CANChannel: cfg.Channel,
		DeviceType: deviceType,
		DeviceInd:  deviceInd,
		ctx:        ctx,
		cancel:     cancel,
		cfg:        cfg,
	}
}

func (c *CanalystII) Init() error {
	c.lifecycle.opMu.Lock()
	defer c.lifecycle.opMu.Unlock()
	if c.lifecycle.isInitialized() {
		return nil
	}

	cfg, err := normalizeConfig(c.cfg)
	if err != nil {
		return err
	}
	if cfg.Mode != CAN {
		return errors.New("CanalystII supports classic CAN only")
	}
	c.cfg = cfg
	c.CANChannel = cfg.Channel
	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.rxChan = make(chan CanFrame, cfg.RxBufferSize)
	c.fanout = newRxFanout(c.ctx, c.rxChan, c.resetTelemetry())

	cleanup := func(err error) error {
		if c.opened && c.closeDeviceProc != nil {
			_, _, _ = c.closeDeviceProc.Call(uintptr(c.DeviceType), uintptr(c.DeviceInd))
			c.opened = false
		}
		if c.cancel != nil {
			c.cancel()
		}
		if c.fanout != nil {
			c.fanout.Close()
			c.fanout = nil
		}
		if c.rxChan != nil {
			close(c.rxChan)
			c.rxChan = nil
		}
		c.closeTelemetry()
		return err
	}

	if err := c.loadDLL(); err != nil {
		return cleanup(err)
	}

	timing, err := canalystTiming(cfg.NominalBitrate)
	if err != nil {
		return cleanup(err)
	}

	ret, _, _ := c.openDeviceProc.Call(
		uintptr(c.DeviceType),
		uintptr(c.DeviceInd),
		0, // Reserved
	)
	if ret != STATUS_OK {
		return cleanup(errors.New("VCI_OpenDevice failed"))
	}
	c.opened = true

	initCfg := VCI_INIT_CONFIG{
		AccCode: 0,
		AccMask: 0xFFFFFFFF, // accept all
		Filter:  0,
		Timing0: timing.Btr0,
		Timing1: timing.Btr1,
		Mode:    0, // normal
	}
	ret, _, _ = c.initCANProc.Call(
		uintptr(c.DeviceType),
		uintptr(c.DeviceInd),
		uintptr(c.CANChannel),
		uintptr(unsafe.Pointer(&initCfg)),
	)
	if ret != STATUS_OK {
		return cleanup(errors.New("VCI_InitCAN failed"))
	}

	if c.clearBufferProc != nil {
		_, _, _ = c.clearBufferProc.Call(
			uintptr(c.DeviceType),
			uintptr(c.DeviceInd),
			uintptr(c.CANChannel),
		)
	}

	ret, _, _ = c.startCANProc.Call(
		uintptr(c.DeviceType),
		uintptr(c.DeviceInd),
		uintptr(c.CANChannel),
	)
	if ret != STATUS_OK {
		return cleanup(errors.New("VCI_StartCAN failed"))
	}

	c.lifecycle.markInitialized()
	log.Println("CanalystII driver initialized successfully")
	return nil
}

func (c *CanalystII) Start() {
	if err := c.StartWithError(); err != nil {
		log.Printf("CanalystII start failed: %v", err)
	}
}

func (c *CanalystII) StartWithError() error {
	c.lifecycle.opMu.Lock()
	defer c.lifecycle.opMu.Unlock()
	if !c.lifecycle.isInitialized() || !c.opened {
		return fmt.Errorf("%w: CanalystII", ErrDriverNotInitialized)
	}
	if c.lifecycle.start(c.readLoop) {
		log.Println("CanalystII can_driver started")
	}
	return nil
}

func (c *CanalystII) readLoop() {
	ticker := time.NewTicker(c.cfg.PollingInterval)
	defer ticker.Stop()
	var canMsg [MsgBufferSize]VCI_CAN_OBJ
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			n, _, _ := c.receiveProc.Call(
				uintptr(c.DeviceType),
				uintptr(c.DeviceInd),
				uintptr(c.CANChannel),
				uintptr(unsafe.Pointer(&canMsg[0])),
				uintptr(MsgBufferSize),
				0,
			)
			// VCI_Receive returns the frame count; 0xFFFFFFFF (-1 as ULONG) means device error.
			count := int32(n)
			if count < 0 {
				log.Printf("CanalystII VCI_Receive device error")
				continue
			}
			if count == 0 {
				continue
			}
			if count > MsgBufferSize {
				log.Printf("CanalystII returned invalid receive count %d", count)
				continue
			}
			for i := 0; i < int(count); i++ {
				msg := canMsg[i]
				if msg.RemoteFlag != 0 || msg.ExternFlag != 0 {
					continue
				}
				if msg.ID > 0x7FF {
					continue
				}
				if msg.DataLen > 8 {
					continue
				}
				var unifiedMsg CanFrame
				unifiedMsg.Direction = RX
				unifiedMsg.ID = msg.ID
				unifiedMsg.DLC = msg.DataLen
				unifiedMsg.IsFD = false
				copy(unifiedMsg.Data[:], msg.Data[:msg.DataLen])
				logCANMessage("RX", unifiedMsg.ID, unifiedMsg.DLC, unifiedMsg.Data[:msg.DataLen], CAN)
				c.publishRx(c.ctx, c.rxChan, unifiedMsg)
			}
		}
	}
}

func (c *CanalystII) Stop() {
	c.lifecycle.opMu.Lock()
	defer c.lifecycle.opMu.Unlock()
	wasInitialized := c.lifecycle.cancelAndWait(c.cancel)

	if c.fanout != nil {
		c.fanout.Close()
		c.fanout = nil
	}

	if wasInitialized && c.opened && c.closeDeviceProc != nil {
		_, _, _ = c.closeDeviceProc.Call(uintptr(c.DeviceType), uintptr(c.DeviceInd))
		c.opened = false
	}

	if c.rxChan != nil {
		close(c.rxChan)
		c.rxChan = nil
	}
	c.closeTelemetry()
	log.Println("CanalystII stopped")
}

func (c *CanalystII) Write(id int32, fd bool, data []byte) error {
	c.lifecycle.opMu.Lock()
	defer c.lifecycle.opMu.Unlock()
	if !c.lifecycle.isInitialized() || !c.opened {
		return errors.New("CanalystII driver is not initialized")
	}
	if fd {
		return errors.New("CanalystII does not support CAN-FD")
	}
	if err := validateWrite(c.cfg, id, fd, data); err != nil {
		return err
	}

	var msg VCI_CAN_OBJ
	msg.ID = uint32(id)
	msg.SendType = 0 // normal send with retry
	msg.RemoteFlag = 0
	msg.ExternFlag = 0
	msg.DataLen = uint8(len(data))
	copy(msg.Data[:], data)

	ret, _, _ := c.transmitProc.Call(
		uintptr(c.DeviceType),
		uintptr(c.DeviceInd),
		uintptr(c.CANChannel),
		uintptr(unsafe.Pointer(&msg)),
		1,
	)
	// VCI_Transmit returns the number of frames successfully sent.
	if ret != 1 {
		return fmt.Errorf("VCI_Transmit failed: sent %d of 1", ret)
	}
	logCANMessage("TX", msg.ID, msg.DataLen, msg.Data[:msg.DataLen], CAN)
	return nil
}

func (c *CanalystII) RxChan() <-chan CanFrame {
	c.lifecycle.opMu.Lock()
	defer c.lifecycle.opMu.Unlock()
	if c.fanout == nil {
		return nil
	}
	ch, _ := c.fanout.Subscribe(c.cfg.RxBufferSize)
	return ch
}

func (c *CanalystII) SubscribeRx(buffer int) (<-chan CanFrame, func()) {
	c.lifecycle.opMu.Lock()
	defer c.lifecycle.opMu.Unlock()
	if c.fanout == nil {
		return nil, func() {}
	}
	return c.fanout.Subscribe(buffer)
}

func (c *CanalystII) IsFDMode() bool {
	return false
}

func (c *CanalystII) Config() Config {
	c.lifecycle.opMu.Lock()
	defer c.lifecycle.opMu.Unlock()
	return c.cfg
}

func (c *CanalystII) loadDLL() error {
	if c.dll != nil {
		return nil
	}
	candidates := []string{
		filepath.Join(".", "bin", canalystDLLName),
		canalystDLLName,
	}
	var errs []string
	for _, dllPath := range candidates {
		dll := syscall.NewLazyDLL(dllPath)
		if err := dll.Load(); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", dllPath, err))
			continue
		}
		c.dll = dll
		c.openDeviceProc = dll.NewProc("VCI_OpenDevice")
		c.closeDeviceProc = dll.NewProc("VCI_CloseDevice")
		c.initCANProc = dll.NewProc("VCI_InitCAN")
		c.startCANProc = dll.NewProc("VCI_StartCAN")
		c.clearBufferProc = dll.NewProc("VCI_ClearBuffer")
		c.transmitProc = dll.NewProc("VCI_Transmit")
		c.receiveProc = dll.NewProc("VCI_Receive")
		return nil
	}
	return fmt.Errorf("failed to load %s (%s)", canalystDLLName, strings.Join(errs, "; "))
}

func canalystTiming(bitrate uint32) (Btr, error) {
	if timing, ok := canalystBitrateTable[bitrate]; ok {
		return timing, nil
	}
	return Btr{}, fmt.Errorf("unsupported CanalystII bitrate: %d", bitrate)
}

