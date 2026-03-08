//go:build darwin && cgo

package driver

/*
#include <dlfcn.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
	uint8_t Mode;
	uint8_t ISOCRCEnable;
	uint8_t RetrySend;
	uint8_t ResEnable;
	uint8_t NBT_BRP;
	uint8_t NBT_SEG1;
	uint8_t NBT_SEG2;
	uint8_t NBT_SJW;
	uint8_t DBT_BRP;
	uint8_t DBT_SEG1;
	uint8_t DBT_SEG2;
	uint8_t DBT_SJW;
	uint8_t __Res0[8];
} CANFD_INIT_CONFIG;

typedef struct {
	uint32_t ID;
	uint8_t DLC;
	uint8_t Flags;
	uint8_t __Res0;
	uint8_t TimeStampHigh;
	uint32_t TimeStamp;
	uint8_t Data[64];
} CANFD_MSG;

typedef int (*fn_USB_ScanDevice)(int* pDevHandle);
typedef bool (*fn_USB_OpenDevice)(int DevHandle);
typedef bool (*fn_USB_CloseDevice)(int DevHandle);
typedef int (*fn_CANFD_Init)(int DevHandle, uint8_t CANIndex, CANFD_INIT_CONFIG* pCanConfig);
typedef int (*fn_CANFD_StartGetMsg)(int DevHandle, uint8_t CANIndex);
typedef int (*fn_CANFD_GetMsg)(int DevHandle, uint8_t CANIndex, CANFD_MSG* pCanMsg, int MsgBufferSize);
typedef int (*fn_CANFD_SendMsg)(int DevHandle, uint8_t CANIndex, CANFD_MSG* pCanMsg, uint32_t SendMsgNum);
typedef int (*fn_CANFD_GetCANSpeedArg)(int DevHandle, CANFD_INIT_CONFIG* pCanConfig, uint32_t ABitrate, uint32_t DBitrate);

static void* g_libusb = NULL;
static void* g_usb2xxx = NULL;

static fn_USB_ScanDevice pUSB_ScanDevice = NULL;
static fn_USB_OpenDevice pUSB_OpenDevice = NULL;
static fn_USB_CloseDevice pUSB_CloseDevice = NULL;
static fn_CANFD_Init pCANFD_Init = NULL;
static fn_CANFD_StartGetMsg pCANFD_StartGetMsg = NULL;
static fn_CANFD_GetMsg pCANFD_GetMsg = NULL;
static fn_CANFD_SendMsg pCANFD_SendMsg = NULL;
static fn_CANFD_GetCANSpeedArg pCANFD_GetCANSpeedArg = NULL;

void toomoss_unload();

static int write_error(char* errbuf, size_t errlen, const char* prefix, const char* detail) {
	if (errbuf != NULL && errlen > 0) {
		if (detail == NULL) {
			snprintf(errbuf, errlen, "%s", prefix);
		} else {
			snprintf(errbuf, errlen, "%s: %s", prefix, detail);
		}
	}
	return -1;
}

#define LOAD_SYMBOL(dst, type, name, errbuf, errlen) \
	do { \
		dlerror(); \
		dst = (type)dlsym(g_usb2xxx, name); \
		const char* sym_err = dlerror(); \
		if (sym_err != NULL || dst == NULL) { \
			toomoss_unload(); \
			return write_error(errbuf, errlen, name, sym_err); \
		} \
	} while (0)

int toomoss_load(const char* libusb_path, const char* usb2xxx_path, char* errbuf, size_t errlen) {
	if (g_usb2xxx != NULL) {
		return 0;
	}

	if (errbuf != NULL && errlen > 0) {
		errbuf[0] = '\0';
	}

	g_libusb = dlopen(libusb_path, RTLD_NOW | RTLD_GLOBAL);
	if (g_libusb == NULL) {
		return write_error(errbuf, errlen, "dlopen libusb-1.0.0.dylib failed", dlerror());
	}

	g_usb2xxx = dlopen(usb2xxx_path, RTLD_NOW | RTLD_GLOBAL);
	if (g_usb2xxx == NULL) {
		const char* err = dlerror();
		dlclose(g_libusb);
		g_libusb = NULL;
		return write_error(errbuf, errlen, "dlopen libUSB2XXX.dylib failed", err);
	}

	LOAD_SYMBOL(pUSB_ScanDevice, fn_USB_ScanDevice, "USB_ScanDevice", errbuf, errlen);
	LOAD_SYMBOL(pUSB_OpenDevice, fn_USB_OpenDevice, "USB_OpenDevice", errbuf, errlen);
	LOAD_SYMBOL(pUSB_CloseDevice, fn_USB_CloseDevice, "USB_CloseDevice", errbuf, errlen);
	LOAD_SYMBOL(pCANFD_Init, fn_CANFD_Init, "CANFD_Init", errbuf, errlen);
	LOAD_SYMBOL(pCANFD_StartGetMsg, fn_CANFD_StartGetMsg, "CANFD_StartGetMsg", errbuf, errlen);
	LOAD_SYMBOL(pCANFD_GetMsg, fn_CANFD_GetMsg, "CANFD_GetMsg", errbuf, errlen);
	LOAD_SYMBOL(pCANFD_SendMsg, fn_CANFD_SendMsg, "CANFD_SendMsg", errbuf, errlen);
	LOAD_SYMBOL(pCANFD_GetCANSpeedArg, fn_CANFD_GetCANSpeedArg, "CANFD_GetCANSpeedArg", errbuf, errlen);

	return 0;
}

void toomoss_unload() {
	pUSB_ScanDevice = NULL;
	pUSB_OpenDevice = NULL;
	pUSB_CloseDevice = NULL;
	pCANFD_Init = NULL;
	pCANFD_StartGetMsg = NULL;
	pCANFD_GetMsg = NULL;
	pCANFD_SendMsg = NULL;
	pCANFD_GetCANSpeedArg = NULL;

	if (g_usb2xxx != NULL) {
		dlclose(g_usb2xxx);
		g_usb2xxx = NULL;
	}
	if (g_libusb != NULL) {
		dlclose(g_libusb);
		g_libusb = NULL;
	}
}

int toomoss_usb_scan_device(int* pDevHandle) {
	if (pUSB_ScanDevice == NULL) return -1;
	return pUSB_ScanDevice(pDevHandle);
}

int toomoss_usb_open_device(int DevHandle) {
	if (pUSB_OpenDevice == NULL) return -1;
	return pUSB_OpenDevice(DevHandle);
}

int toomoss_usb_close_device(int DevHandle) {
	if (pUSB_CloseDevice == NULL) return -1;
	return pUSB_CloseDevice(DevHandle);
}

int toomoss_canfd_init(int DevHandle, uint8_t CANIndex, CANFD_INIT_CONFIG* pCanConfig) {
	if (pCANFD_Init == NULL) return -1;
	return pCANFD_Init(DevHandle, CANIndex, pCanConfig);
}

int toomoss_canfd_start_get_msg(int DevHandle, uint8_t CANIndex) {
	if (pCANFD_StartGetMsg == NULL) return -1;
	return pCANFD_StartGetMsg(DevHandle, CANIndex);
}

int toomoss_canfd_get_msg(int DevHandle, uint8_t CANIndex, CANFD_MSG* pCanMsg, int MsgBufferSize) {
	if (pCANFD_GetMsg == NULL) return -1;
	return pCANFD_GetMsg(DevHandle, CANIndex, pCanMsg, MsgBufferSize);
}

int toomoss_canfd_send_msg(int DevHandle, uint8_t CANIndex, CANFD_MSG* pCanMsg, uint32_t SendMsgNum) {
	if (pCANFD_SendMsg == NULL) return -1;
	return pCANFD_SendMsg(DevHandle, CANIndex, pCanMsg, SendMsgNum);
}

int toomoss_canfd_get_speed_arg(int DevHandle, CANFD_INIT_CONFIG* pCanConfig, uint32_t ABitrate, uint32_t DBitrate) {
	if (pCANFD_GetCANSpeedArg == NULL) return -1;
	return pCANFD_GetCANSpeedArg(DevHandle, pCanConfig, ABitrate, DBitrate);
}
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
	"unsafe"
)

const (
	toomossLibusbPath = "/Applications/TCANLINPro.app/Contents/Frameworks/libusb-1.0.0.dylib"
	toomossUSB2XXX    = "/Applications/TCANLINPro.app/Contents/Frameworks/libUSB2XXX.dylib"
)

var (
	DevHandle [10]C.int
	DEVIndex  = 0

	toomossMu     sync.Mutex
	toomossLoaded bool
)

func resetToomossState() {
	DevHandle = [10]C.int{}
	toomossLoaded = false
}

func ensureToomossLoaded() error {
	toomossMu.Lock()
	defer toomossMu.Unlock()

	if toomossLoaded {
		return nil
	}

	libusbPath := C.CString(toomossLibusbPath)
	defer C.free(unsafe.Pointer(libusbPath))

	usb2xxxPath := C.CString(toomossUSB2XXX)
	defer C.free(unsafe.Pointer(usb2xxxPath))

	var errBuf [512]C.char
	if ret := C.toomoss_load(libusbPath, usb2xxxPath, &errBuf[0], C.size_t(len(errBuf))); ret != 0 {
		return fmt.Errorf("load Toomoss dylib failed: %s", C.GoString(&errBuf[0]))
	}

	toomossLoaded = true
	return nil
}

func usbScan() (bool, error) {
	if err := ensureToomossLoaded(); err != nil {
		return false, err
	}

	ret := int(C.toomoss_usb_scan_device(&DevHandle[DEVIndex]))
	if ret <= 0 {
		return false, nil
	}
	return true, nil
}

func UsbScan() bool {
	ok, err := usbScan()
	if err != nil {
		log.Printf("USB scan failed: %v", err)
		return false
	}
	return ok
}

func usbOpen() (bool, error) {
	if err := ensureToomossLoaded(); err != nil {
		return false, err
	}

	stateValue := int(C.toomoss_usb_open_device(DevHandle[DEVIndex]))
	return stateValue >= 1, nil
}

func UsbOpen() bool {
	ok, err := usbOpen()
	if err != nil {
		log.Printf("USB open failed: %v", err)
		return false
	}
	return ok
}

func usbClose() error {
	toomossMu.Lock()
	defer toomossMu.Unlock()

	if !toomossLoaded {
		return nil
	}

	ret := int(C.toomoss_usb_close_device(DevHandle[DEVIndex]))
	if ret < 1 {
		return fmt.Errorf("USB_CloseDevice returned %d", ret)
	}

	C.toomoss_unload()
	resetToomossState()
	return nil
}

func UsbClose() bool {
	if err := usbClose(); err != nil {
		log.Printf("USB close failed: %v", err)
		return false
	}
	return true
}

const (
	SpeedBpsNBT = 500_000
	SpeedBpsDBT = 2_000_000
)

const (
	CAN_MSG_FLAG_STD   byte = 0
	CANFD_MSG_FLAG_BRS byte = 1 << 0 // CANFD加速帧标志
	CANFD_MSG_FLAG_ESI byte = 1 << 1 // CANFD错误状态指示
	CANFD_MSG_FLAG_FDF byte = 1 << 2 // CANFD帧标志
)

type Toomoss struct {
	rxChan     chan UnifiedCANMessage
	fanout     *rxFanout
	ctx        context.Context
	cancel     context.CancelFunc
	canType    CanType
	CANChannel byte
}

func NewToomoss(canType CanType, canChannel byte) *Toomoss {
	rxChan := make(chan UnifiedCANMessage, RxChannelBufferSize)
	ctx, cancel := context.WithCancel(context.Background())
	return &Toomoss{
		rxChan:     rxChan,
		fanout:     newRxFanout(ctx, rxChan),
		ctx:        ctx,
		cancel:     cancel,
		canType:    canType,
		CANChannel: canChannel,
	}
}

func (c *Toomoss) Init() error {
	if err := ensureToomossLoaded(); err != nil {
		return err
	}

	if ok, err := usbScan(); err != nil {
		return fmt.Errorf("USB scan failed: %w", err)
	} else if !ok {
		return errors.New("USB scan failed: device not found")
	}
	if ok, err := usbOpen(); err != nil {
		return fmt.Errorf("USB open failed: %w", err)
	} else if !ok {
		return errors.New("USB open failed")
	}

	canFDInitConfig := C.CANFD_INIT_CONFIG{
		Mode:         C.uint8_t(0),
		ISOCRCEnable: C.uint8_t(1),
		RetrySend:    C.uint8_t(1),
		ResEnable:    C.uint8_t(1),
		NBT_BRP:      C.uint8_t(1),
		NBT_SEG1:     C.uint8_t(59),
		NBT_SEG2:     C.uint8_t(20),
		NBT_SJW:      C.uint8_t(2),
		DBT_BRP:      C.uint8_t(1),
		DBT_SEG1:     C.uint8_t(14),
		DBT_SEG2:     C.uint8_t(5),
		DBT_SJW:      C.uint8_t(2),
	}

	fdSpeed := int(C.toomoss_canfd_get_speed_arg(
		DevHandle[DEVIndex],
		&canFDInitConfig,
		C.uint32_t(SpeedBpsNBT),
		C.uint32_t(SpeedBpsDBT),
	))
	canfdInit := int(C.toomoss_canfd_init(
		DevHandle[DEVIndex],
		C.uint8_t(c.CANChannel),
		&canFDInitConfig,
	))
	fdStart := int(C.toomoss_canfd_start_get_msg(
		DevHandle[DEVIndex],
		C.uint8_t(c.CANChannel),
	))

	time.Sleep(InitDelay)
	if !(canfdInit == 0 && fdStart == 0 && fdSpeed == 0) {
		log.Println("错误: CAN硬件初始化失败！")
		return fmt.Errorf("错误: CAN硬件初始化失败")
	}
	log.Println("CAN硬件初始化成功。")
	return nil
}

func (c *Toomoss) Start() {
	log.Println("CAN-FD驱动的中央读取服务已启动...")
	c.drainInitialBuffer()
	go c.readLoop()
}

func (c *Toomoss) Stop() {
	log.Println("正在停止CAN-FD驱动的读取服务...")
	c.cancel()
	if c.fanout != nil {
		c.fanout.Close()
	}
	if err := usbClose(); err != nil {
		log.Printf("警告: USB关闭失败: %v", err)
	}
}

func (c *Toomoss) readLoop() {
	ticker := time.NewTicker(PollingInterval)
	defer ticker.Stop()

	var canFDMsg [MsgBufferSize]C.CANFD_MSG

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			getCanFDMsgNum := int(C.toomoss_canfd_get_msg(
				DevHandle[DEVIndex],
				C.uint8_t(c.CANChannel),
				&canFDMsg[0],
				C.int(len(canFDMsg)),
			))

			if getCanFDMsgNum <= 0 {
				continue
			}

			for i := 0; i < getCanFDMsgNum; i++ {
				msg := canFDMsg[i]
				dlc := byte(msg.DLC)
				actualLen := dlcToLen(dlc)
				if actualLen == 0 {
					continue
				}

				var payload [64]byte
				for j := 0; j < actualLen && j < len(payload); j++ {
					payload[j] = byte(msg.Data[j])
				}

				flags := byte(msg.Flags)
				unifiedMsg := UnifiedCANMessage{
					Direction: RX,
					ID:        uint32(msg.ID),
					DLC:       dlc,
					Data:      payload,
					IsFD:      flags&CANFD_MSG_FLAG_FDF != 0,
				}

				msgType := c.canType
				if flags == CAN_MSG_FLAG_STD {
					msgType = CAN
				} else {
					msgType = CANFD
				}
				logCANMessage("RX", unifiedMsg.ID, dataLenToDlc(int(unifiedMsg.DLC)), unifiedMsg.Data[:unifiedMsg.DLC], msgType)

				select {
				case c.rxChan <- unifiedMsg:
				default:
					log.Println("警告: 驱动接收channel(FD)已满，消息被丢弃")
				}
			}
		}
	}
}

func (c *Toomoss) drainInitialBuffer() {
	var canFDMsg [MsgBufferSize]C.CANFD_MSG
	for {
		n := int(C.toomoss_canfd_get_msg(
			DevHandle[DEVIndex],
			C.uint8_t(c.CANChannel),
			&canFDMsg[0],
			C.int(len(canFDMsg)),
		))
		if n <= 0 {
			break
		}
	}
}

func (c *Toomoss) Write(id int32, data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("数据长度 %d", len(data))
	}
	if len(data) > 64 && c.canType == CANFD {
		return fmt.Errorf("数据长度 %d 超过CAN-FD最大长度64", len(data))
	}
	if len(data) > 8 && c.canType == CAN {
		return fmt.Errorf("数据长度 %d 超过CAN最大长度8", len(data))
	}

	var msg C.CANFD_MSG
	msg.ID = C.uint32_t(id)
	msg.DLC = C.uint8_t(len(data))
	switch c.canType {
	case CAN:
		msg.Flags = C.uint8_t(CAN_MSG_FLAG_STD)
	case CANFD:
		msg.Flags = C.uint8_t(CANFD_MSG_FLAG_FDF)
	default:
		msg.Flags = C.uint8_t(CANFD_MSG_FLAG_FDF)
	}
	for i := 0; i < len(data) && i < 64; i++ {
		msg.Data[i] = C.uint8_t(data[i])
	}

	sendRet := int(C.toomoss_canfd_send_msg(
		DevHandle[DEVIndex],
		C.uint8_t(c.CANChannel),
		&msg,
		C.uint32_t(1),
	))

	if sendRet == 1 {
		var payload [64]byte
		copy(payload[:], data)

		unifiedMsg := UnifiedCANMessage{
			Direction: TX,
			ID:        uint32(msg.ID),
			DLC:       byte(msg.DLC),
			Data:      payload,
			IsFD:      byte(msg.Flags)&CANFD_MSG_FLAG_FDF != 0,
		}

		logCANMessage("TX", uint32(id), byte(msg.DLC), payload[:len(data)], c.canType)
		select {
		case c.rxChan <- unifiedMsg:
		default:
			log.Println("警告: 驱动接收channel(FD)已满，消息被丢弃")
		}
		return nil
	}

	log.Printf("错误: CAN/CANFD消息发送失败, ID=0x%03X", id)
	return errors.New("CAN/CANFD消息发送失败")
}

func (c *Toomoss) RxChan() <-chan UnifiedCANMessage {
	if c.fanout == nil {
		return nil
	}
	return c.fanout.Subscribe(RxChannelBufferSize)
}

func (c *Toomoss) Context() context.Context { return c.ctx }
