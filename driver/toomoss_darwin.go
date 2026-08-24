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

typedef struct {
	uint32_t CAN_BRP;
	uint8_t CAN_SJW;
	uint8_t CAN_BS1;
	uint8_t CAN_BS2;
	uint8_t CAN_Mode;
	uint8_t CAN_ABOM;
	uint8_t CAN_NART;
	uint8_t CAN_RFLM;
	uint8_t CAN_TXFP;
} CAN_INIT_CONFIG;

typedef struct {
	uint32_t ID;
	uint32_t TimeStamp;
	uint8_t RemoteFlag;
	uint8_t ExternFlag;
	uint8_t DataLen;
	uint8_t Data[8];
	uint8_t TimeStampHigh;
} CAN_MSG;

typedef int (*fn_USB_ScanDevice)(int* pDevHandle);
typedef bool (*fn_USB_OpenDevice)(int DevHandle);
typedef bool (*fn_USB_CloseDevice)(int DevHandle);
typedef int (*fn_CAN_Init)(int DevHandle, uint8_t CANIndex, CAN_INIT_CONFIG* pCanConfig);
typedef int (*fn_CAN_StartGetMsg)(int DevHandle, uint8_t CANIndex);
typedef int (*fn_CAN_GetMsg)(int DevHandle, uint8_t CANIndex, CAN_MSG* pCanMsg);
typedef int (*fn_CAN_SendMsg)(int DevHandle, uint8_t CANIndex, CAN_MSG* pCanMsg, uint32_t SendMsgNum);
typedef int (*fn_CAN_GetCANSpeedArg)(int DevHandle, CAN_INIT_CONFIG* pCanConfig, uint32_t BaudRate);
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
static fn_CAN_Init pCAN_Init = NULL;
static fn_CAN_StartGetMsg pCAN_StartGetMsg = NULL;
static fn_CAN_GetMsg pCAN_GetMsg = NULL;
static fn_CAN_SendMsg pCAN_SendMsg = NULL;
static fn_CAN_GetCANSpeedArg pCAN_GetCANSpeedArg = NULL;
static fn_CANFD_Init pCANFD_Init = NULL;
static fn_CANFD_StartGetMsg pCANFD_StartGetMsg = NULL;
static fn_CANFD_GetMsg pCANFD_GetMsg = NULL;
static fn_CANFD_SendMsg pCANFD_SendMsg = NULL;
static fn_CANFD_GetCANSpeedArg pCANFD_GetCANSpeedArg = NULL;

void can_toomoss_unload();

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
			can_toomoss_unload(); \
			return write_error(errbuf, errlen, name, sym_err); \
		} \
	} while (0)

#define LOAD_OPTIONAL_SYMBOL(dst, type, name) \
	do { \
		dlerror(); \
		dst = (type)dlsym(g_usb2xxx, name); \
		if (dlerror() != NULL) dst = NULL; \
	} while (0)

int can_toomoss_load(const char* libusb_path, const char* usb2xxx_path, char* errbuf, size_t errlen) {
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
	LOAD_OPTIONAL_SYMBOL(pCAN_Init, fn_CAN_Init, "CAN_Init");
	LOAD_OPTIONAL_SYMBOL(pCAN_StartGetMsg, fn_CAN_StartGetMsg, "CAN_StartGetMsg");
	LOAD_OPTIONAL_SYMBOL(pCAN_GetMsg, fn_CAN_GetMsg, "CAN_GetMsg");
	LOAD_OPTIONAL_SYMBOL(pCAN_SendMsg, fn_CAN_SendMsg, "CAN_SendMsg");
	LOAD_OPTIONAL_SYMBOL(pCAN_GetCANSpeedArg, fn_CAN_GetCANSpeedArg, "CAN_GetCANSpeedArg");
	LOAD_OPTIONAL_SYMBOL(pCANFD_Init, fn_CANFD_Init, "CANFD_Init");
	LOAD_OPTIONAL_SYMBOL(pCANFD_StartGetMsg, fn_CANFD_StartGetMsg, "CANFD_StartGetMsg");
	LOAD_OPTIONAL_SYMBOL(pCANFD_GetMsg, fn_CANFD_GetMsg, "CANFD_GetMsg");
	LOAD_OPTIONAL_SYMBOL(pCANFD_SendMsg, fn_CANFD_SendMsg, "CANFD_SendMsg");
	LOAD_OPTIONAL_SYMBOL(pCANFD_GetCANSpeedArg, fn_CANFD_GetCANSpeedArg, "CANFD_GetCANSpeedArg");

	if (!((pCAN_Init != NULL && pCAN_StartGetMsg != NULL && pCAN_GetMsg != NULL &&
		   pCAN_SendMsg != NULL && pCAN_GetCANSpeedArg != NULL) ||
		  (pCANFD_Init != NULL && pCANFD_StartGetMsg != NULL && pCANFD_GetMsg != NULL &&
		   pCANFD_SendMsg != NULL && pCANFD_GetCANSpeedArg != NULL))) {
		can_toomoss_unload();
		return write_error(errbuf, errlen, "required Toomoss CAN procedures are not available", NULL);
	}

	return 0;
}

void can_toomoss_unload() {
	pUSB_ScanDevice = NULL;
	pUSB_OpenDevice = NULL;
	pUSB_CloseDevice = NULL;
	pCAN_Init = NULL;
	pCAN_StartGetMsg = NULL;
	pCAN_GetMsg = NULL;
	pCAN_SendMsg = NULL;
	pCAN_GetCANSpeedArg = NULL;
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

int can_toomoss_usb_scan_device(int* pDevHandle) {
	if (pUSB_ScanDevice == NULL) return -1;
	return pUSB_ScanDevice(pDevHandle);
}

int can_toomoss_usb_open_device(int DevHandle) {
	if (pUSB_OpenDevice == NULL) return -1;
	return pUSB_OpenDevice(DevHandle);
}

int can_toomoss_usb_close_device(int DevHandle) {
	if (pUSB_CloseDevice == NULL) return -1;
	return pUSB_CloseDevice(DevHandle);
}

int can_toomoss_has_can() {
	return pCAN_Init != NULL && pCAN_StartGetMsg != NULL && pCAN_GetMsg != NULL &&
		pCAN_SendMsg != NULL && pCAN_GetCANSpeedArg != NULL;
}

int can_toomoss_has_canfd() {
	return pCANFD_Init != NULL && pCANFD_StartGetMsg != NULL && pCANFD_GetMsg != NULL &&
		pCANFD_SendMsg != NULL && pCANFD_GetCANSpeedArg != NULL;
}

int can_toomoss_can_init(int DevHandle, uint8_t CANIndex, CAN_INIT_CONFIG* pCanConfig) {
	if (pCAN_Init == NULL) return -1;
	return pCAN_Init(DevHandle, CANIndex, pCanConfig);
}

int can_toomoss_can_start_get_msg(int DevHandle, uint8_t CANIndex) {
	if (pCAN_StartGetMsg == NULL) return -1;
	return pCAN_StartGetMsg(DevHandle, CANIndex);
}

int can_toomoss_can_get_msg(int DevHandle, uint8_t CANIndex, CAN_MSG* pCanMsg) {
	if (pCAN_GetMsg == NULL) return -1;
	return pCAN_GetMsg(DevHandle, CANIndex, pCanMsg);
}

int can_toomoss_can_send_msg(int DevHandle, uint8_t CANIndex, CAN_MSG* pCanMsg, uint32_t SendMsgNum) {
	if (pCAN_SendMsg == NULL) return -1;
	return pCAN_SendMsg(DevHandle, CANIndex, pCanMsg, SendMsgNum);
}

int can_toomoss_can_get_speed_arg(int DevHandle, CAN_INIT_CONFIG* pCanConfig, uint32_t BaudRate) {
	if (pCAN_GetCANSpeedArg == NULL) return -1;
	return pCAN_GetCANSpeedArg(DevHandle, pCanConfig, BaudRate);
}

int can_toomoss_canfd_init(int DevHandle, uint8_t CANIndex, CANFD_INIT_CONFIG* pCanConfig) {
	if (pCANFD_Init == NULL) return -1;
	return pCANFD_Init(DevHandle, CANIndex, pCanConfig);
}

int can_toomoss_canfd_start_get_msg(int DevHandle, uint8_t CANIndex) {
	if (pCANFD_StartGetMsg == NULL) return -1;
	return pCANFD_StartGetMsg(DevHandle, CANIndex);
}

int can_toomoss_canfd_get_msg(int DevHandle, uint8_t CANIndex, CANFD_MSG* pCanMsg, int MsgBufferSize) {
	if (pCANFD_GetMsg == NULL) return -1;
	return pCANFD_GetMsg(DevHandle, CANIndex, pCanMsg, MsgBufferSize);
}

int can_toomoss_canfd_send_msg(int DevHandle, uint8_t CANIndex, CANFD_MSG* pCanMsg, uint32_t SendMsgNum) {
	if (pCANFD_SendMsg == NULL) return -1;
	return pCANFD_SendMsg(DevHandle, CANIndex, pCanMsg, SendMsgNum);
}

int can_toomoss_canfd_get_speed_arg(int DevHandle, CANFD_INIT_CONFIG* pCanConfig, uint32_t ABitrate, uint32_t DBitrate) {
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

	toomossMu        sync.Mutex
	toomossLoaded    bool
	toomossSessionMu sync.Mutex
	toomossInUse     bool
)

func acquireToomossSession() bool {
	toomossSessionMu.Lock()
	defer toomossSessionMu.Unlock()
	if toomossInUse {
		return false
	}
	toomossInUse = true
	return true
}

func releaseToomossSession() {
	toomossSessionMu.Lock()
	toomossInUse = false
	toomossSessionMu.Unlock()
}

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
	if ret := C.can_toomoss_load(libusbPath, usb2xxxPath, &errBuf[0], C.size_t(len(errBuf))); ret != 0 {
		return fmt.Errorf("load Toomoss dylib failed: %s", C.GoString(&errBuf[0]))
	}

	toomossLoaded = true
	return nil
}

func usbScan() (bool, error) {
	if err := ensureToomossLoaded(); err != nil {
		return false, err
	}

	ret := int(C.can_toomoss_usb_scan_device(&DevHandle[DEVIndex]))
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

	stateValue := int(C.can_toomoss_usb_open_device(DevHandle[DEVIndex]))
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

	ret := int(C.can_toomoss_usb_close_device(DevHandle[DEVIndex]))
	if ret < 1 {
		return fmt.Errorf("USB_CloseDevice returned %d", ret)
	}

	C.can_toomoss_unload()
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

const (
	toomossCANFDIDMaskStandard = 0x7FF
	toomossClassicFlagRemote   = 0x01
	toomossClassicFlagChannel  = 0x60
	toomossClassicFlagTx       = 0x80
	toomossClassicFlagExt      = 0x01
	toomossClassicFlagError    = 0x80
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
	__Res0       [8]byte
}

func defaultCANFDInitConfig() CANFD_INIT_CONFIG {
	return CANFD_INIT_CONFIG{
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
}

// BuildCANFDInitConfig merges shared timing into Toomoss defaults (Mode/RetrySend/...).
func BuildCANFDInitConfig(timing CANFDInitConfig) CANFD_INIT_CONFIG {
	cfg := defaultCANFDInitConfig()
	cfg.NBT_BRP = timing.NBT_BRP
	cfg.NBT_SEG1 = timing.NBT_SEG1
	cfg.NBT_SEG2 = timing.NBT_SEG2
	cfg.NBT_SJW = timing.NBT_SJW
	cfg.DBT_BRP = timing.DBT_BRP
	cfg.DBT_SEG1 = timing.DBT_SEG1
	cfg.DBT_SEG2 = timing.DBT_SEG2
	cfg.DBT_SJW = timing.DBT_SJW
	return cfg
}

func toCCANFDInitConfig(cfg CANFD_INIT_CONFIG) C.CANFD_INIT_CONFIG {
	return C.CANFD_INIT_CONFIG{
		Mode:         C.uint8_t(cfg.Mode),
		ISOCRCEnable: C.uint8_t(cfg.ISOCRCEnable),
		RetrySend:    C.uint8_t(cfg.RetrySend),
		ResEnable:    C.uint8_t(cfg.ResEnable),
		NBT_BRP:      C.uint8_t(cfg.NBT_BRP),
		NBT_SEG1:     C.uint8_t(cfg.NBT_SEG1),
		NBT_SEG2:     C.uint8_t(cfg.NBT_SEG2),
		NBT_SJW:      C.uint8_t(cfg.NBT_SJW),
		DBT_BRP:      C.uint8_t(cfg.DBT_BRP),
		DBT_SEG1:     C.uint8_t(cfg.DBT_SEG1),
		DBT_SEG2:     C.uint8_t(cfg.DBT_SEG2),
		DBT_SJW:      C.uint8_t(cfg.DBT_SJW),
	}
}

func decodeToomossClassicFlags(remoteFlag, externFlag byte) (channel byte, remote bool, extended bool, errorFrame bool, txEcho bool) {
	channel = (remoteFlag & toomossClassicFlagChannel) >> 5
	remote = (remoteFlag & toomossClassicFlagRemote) != 0
	extended = (externFlag & toomossClassicFlagExt) != 0
	errorFrame = (externFlag & toomossClassicFlagError) != 0
	txEcho = (remoteFlag & toomossClassicFlagTx) != 0
	return
}

func encodeToomossClassicFlags(channel byte, extended bool, remote bool) (remoteFlag byte, externFlag byte) {
	remoteFlag = (channel << 5) & toomossClassicFlagChannel
	if remote {
		remoteFlag |= toomossClassicFlagRemote
	}
	if extended {
		externFlag |= toomossClassicFlagExt
	}
	return remoteFlag, externFlag
}

func toomossDLCToDataLen(rawDLC byte, isFD bool) int {
	maxLen := 8
	if isFD {
		maxLen = 64
	}
	actualLen := int(rawDLC)
	if actualLen > maxLen {
		return maxLen
	}
	return actualLen
}

type Toomoss struct {
	driverObservability
	rxChan          chan CanFrame
	fanout          *rxFanout
	ctx             context.Context
	cancel          context.CancelFunc
	cfg             Config
	lifecycle       driverLifecycle
	canType         CanType
	CANChannel      byte
	legacyCAN       bool
	canFDInitConfig CANFD_INIT_CONFIG
	ownsDevice      bool
}

func NewToomoss(canType CanType, canChannel byte) *Toomoss {
	return NewToomossWithConfig(DefaultConfig(canType, canChannel))
}

func NewToomossWithConfig(cfg Config) *Toomoss {
	ctx, cancel := context.WithCancel(context.Background())
	return &Toomoss{
		ctx:             ctx,
		cancel:          cancel,
		cfg:             cfg,
		canType:         cfg.Mode,
		CANChannel:      cfg.Channel,
		canFDInitConfig: defaultCANFDInitConfig(),
	}
}

func (c *Toomoss) SetCANFDInitConfig(cfg CANFD_INIT_CONFIG) {
	c.canFDInitConfig = cfg
}

// SetCANFDTiming applies shared timing fields while keeping Mode/RetrySend/ISOCRC/ResEnable.
func (c *Toomoss) SetCANFDTiming(timing CANFDInitConfig) {
	c.canFDInitConfig.NBT_BRP = timing.NBT_BRP
	c.canFDInitConfig.NBT_SEG1 = timing.NBT_SEG1
	c.canFDInitConfig.NBT_SEG2 = timing.NBT_SEG2
	c.canFDInitConfig.NBT_SJW = timing.NBT_SJW
	c.canFDInitConfig.DBT_BRP = timing.DBT_BRP
	c.canFDInitConfig.DBT_SEG1 = timing.DBT_SEG1
	c.canFDInitConfig.DBT_SEG2 = timing.DBT_SEG2
	c.canFDInitConfig.DBT_SJW = timing.DBT_SJW
}

func (c *Toomoss) Init() error {
	c.lifecycle.opMu.Lock()
	defer c.lifecycle.opMu.Unlock()
	if c.lifecycle.isInitialized() {
		return nil
	}
	cfg, err := normalizeConfig(c.cfg)
	if err != nil {
		return err
	}
	c.cfg = cfg
	c.canType = cfg.Mode
	c.CANChannel = cfg.Channel
	c.legacyCAN = false

	if !acquireToomossSession() {
		return errors.New("another Toomoss driver instance is already using the device")
	}
	c.ownsDevice = true
	opened := false
	cleanup := func(err error) error {
		if opened {
			_ = usbClose()
		}
		if c.ownsDevice {
			releaseToomossSession()
			c.ownsDevice = false
		}
		return err
	}

	if err := ensureToomossLoaded(); err != nil {
		return cleanup(fmt.Errorf("failed to load Toomoss dylibs: %w", err))
	}

	if ok, err := usbScan(); err != nil {
		return cleanup(fmt.Errorf("USB scan failed: %w", err))
	} else if !ok {
		return cleanup(errors.New("USB scan failed: device not found"))
	}
	if ok, err := usbOpen(); err != nil {
		return cleanup(fmt.Errorf("USB open failed: %w", err))
	} else if !ok {
		return cleanup(errors.New("USB open failed"))
	}
	opened = true

	fallback := func(fdErr error) error {
		if err := c.fallbackToLegacyCAN(fdErr); err != nil {
			return cleanup(err)
		}
		c.prepareRuntime()
		c.lifecycle.markInitialized()
		return nil
	}

	if c.canType == CAN {
		c.legacyCAN = true
		log.Println("Toomoss forced classic CAN mode")
		if err := c.initLegacyCANDevice(); err != nil {
			return cleanup(err)
		}
		c.prepareRuntime()
		c.lifecycle.markInitialized()
		return nil
	}
	if int(C.can_toomoss_has_canfd()) == 0 {
		return fallback(errors.New("CAN-FD APIs are not available in libUSB2XXX.dylib"))
	}

	canFDInitConfig := toCCANFDInitConfig(c.canFDInitConfig)
	fdSpeed := int(C.can_toomoss_canfd_get_speed_arg(
		DevHandle[DEVIndex],
		&canFDInitConfig,
		C.uint32_t(c.cfg.NominalBitrate),
		C.uint32_t(c.cfg.DataBitrate),
	))
	canfdInit := int(C.can_toomoss_canfd_init(
		DevHandle[DEVIndex],
		C.uint8_t(c.CANChannel),
		&canFDInitConfig,
	))
	fdStart := int(C.can_toomoss_canfd_start_get_msg(
		DevHandle[DEVIndex],
		C.uint8_t(c.CANChannel),
	))

	time.Sleep(InitDelay)
	if !(canfdInit == 0 && fdStart == 0 && fdSpeed == 0) {
		return fallback(fmt.Errorf(
			"CAN-FD initialization failed: CANFD_Init=%d, CANFD_StartGetMsg=%d, CANFD_GetCANSpeedArg=%d",
			canfdInit, fdStart, fdSpeed,
		))
	}
	c.prepareRuntime()
	c.lifecycle.markInitialized()
	log.Println("CAN硬件初始化成功。")
	return nil
}

func (c *Toomoss) prepareRuntime() {
	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.rxChan = make(chan CanFrame, c.cfg.RxBufferSize)
	c.fanout = newRxFanout(c.ctx, c.rxChan, c.resetTelemetry())
}

func (c *Toomoss) fallbackToLegacyCAN(fdErr error) error {
	log.Printf("Toomoss CAN-FD initialization failed, fallback to classic CAN: %v", fdErr)
	c.legacyCAN = true
	c.canType = CAN
	c.cfg.Mode = CAN
	if err := c.initLegacyCANDevice(); err != nil {
		return fmt.Errorf("CAN-FD initialization failed (%v), fallback classic CAN initialization failed: %w", fdErr, err)
	}
	return nil
}

func (c *Toomoss) initLegacyCANDevice() error {
	if int(C.can_toomoss_has_can()) == 0 {
		return errors.New("standard CAN APIs are not available in libUSB2XXX.dylib")
	}

	canInitConfig := C.CAN_INIT_CONFIG{
		CAN_Mode: C.uint8_t(0),
		CAN_ABOM: C.uint8_t(0),
		CAN_NART: C.uint8_t(1),
		CAN_RFLM: C.uint8_t(0),
		CAN_TXFP: C.uint8_t(1),
		CAN_BRP:  C.uint32_t(4),
		CAN_BS1:  C.uint8_t(15),
		CAN_BS2:  C.uint8_t(5),
		CAN_SJW:  C.uint8_t(2),
	}
	speedRet := int(C.can_toomoss_can_get_speed_arg(
		DevHandle[DEVIndex],
		&canInitConfig,
		C.uint32_t(c.cfg.NominalBitrate),
	))
	if speedRet != 0 {
		return fmt.Errorf("CAN_GetCANSpeedArg returned %d", speedRet)
	}
	initRet := int(C.can_toomoss_can_init(
		DevHandle[DEVIndex],
		C.uint8_t(c.CANChannel),
		&canInitConfig,
	))
	startRet := int(C.can_toomoss_can_start_get_msg(
		DevHandle[DEVIndex],
		C.uint8_t(c.CANChannel),
	))
	time.Sleep(InitDelay)
	if initRet != 0 || startRet != 0 {
		return fmt.Errorf("standard CAN initialization failed: CAN_Init=%d, CAN_StartGetMsg=%d", initRet, startRet)
	}
	log.Println("Toomoss legacy CAN hardware initialized successfully")
	return nil
}

func (c *Toomoss) Start() {
	if err := c.StartWithError(); err != nil {
		log.Printf("Toomoss start failed: %v", err)
	}
}

func (c *Toomoss) StartWithError() error {
	c.lifecycle.opMu.Lock()
	defer c.lifecycle.opMu.Unlock()
	if !c.lifecycle.isInitialized() {
		return fmt.Errorf("%w: Toomoss", ErrDriverNotInitialized)
	}
	c.drainInitialBuffer()
	if c.lifecycle.start(c.readLoop) {
		log.Println("CAN驱动的中央读取服务已启动...")
	}
	return nil
}

func (c *Toomoss) Stop() {
	c.lifecycle.opMu.Lock()
	defer c.lifecycle.opMu.Unlock()
	log.Println("正在停止CAN-FD驱动的读取服务...")
	wasInitialized := c.lifecycle.cancelAndWait(c.cancel)
	if c.fanout != nil {
		c.fanout.Close()
		c.fanout = nil
	}
	if wasInitialized {
		if err := usbClose(); err != nil {
			log.Printf("警告: USB关闭失败: %v", err)
		}
	}
	if c.rxChan != nil {
		close(c.rxChan)
		c.rxChan = nil
	}
	c.closeTelemetry()
	if c.ownsDevice {
		releaseToomossSession()
		c.ownsDevice = false
	}
}

func (c *Toomoss) readLoop() {
	ticker := time.NewTicker(c.cfg.PollingInterval)
	defer ticker.Stop()

	var canMsg [MsgBufferSize]C.CAN_MSG
	var canFDMsg [MsgBufferSize]C.CANFD_MSG

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if c.legacyCAN {
				c.readClassicBurst(&canMsg)
				continue
			}
			getCanFDMsgNum := int(C.can_toomoss_canfd_get_msg(
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
				if uint32(msg.ID) > toomossCANFDIDMaskStandard {
					continue
				}
				flags := byte(msg.Flags)
				isFD := flags&CANFD_MSG_FLAG_FDF != 0
				actualLen := toomossDLCToDataLen(byte(msg.DLC), isFD)
				dlc := dataLenToDlc(actualLen)

				var payload [64]byte
				for j := 0; j < actualLen && j < len(payload); j++ {
					payload[j] = byte(msg.Data[j])
				}

				unifiedMsg := CanFrame{
					Direction: RX,
					ID:        uint32(msg.ID),
					DLC:       dlc,
					Data:      payload,
					IsFD:      isFD,
				}

				msgType := c.canType
				if flags == CAN_MSG_FLAG_STD {
					msgType = CAN
				} else {
					msgType = CANFD
				}
				logCANMessage("RX", unifiedMsg.ID, unifiedMsg.DLC, unifiedMsg.Data[:actualLen], msgType)
				c.publishRx(c.ctx, c.rxChan, unifiedMsg)
			}
		}
	}
}

func (c *Toomoss) readClassicBurst(canMsg *[MsgBufferSize]C.CAN_MSG) {
	getCANMsgNum := int(C.can_toomoss_can_get_msg(
		DevHandle[DEVIndex],
		C.uint8_t(c.CANChannel),
		&canMsg[0],
	))
	if getCANMsgNum <= 0 {
		return
	}
	if getCANMsgNum > len(canMsg) {
		getCANMsgNum = len(canMsg)
	}

	for i := 0; i < getCANMsgNum; i++ {
		msg := canMsg[i]
		_, remote, extended, errorFrame, txEcho := decodeToomossClassicFlags(byte(msg.RemoteFlag), byte(msg.ExternFlag))
		if errorFrame || extended || remote {
			continue
		}
		direction := RX
		if txEcho {
			if !c.cfg.IncludeTxEcho {
				continue
			}
			direction = TX
		}
		actualLen := int(msg.DataLen)
		if actualLen > len(msg.Data) {
			actualLen = len(msg.Data)
		}
		id := uint32(msg.ID) & toomossCANFDIDMaskStandard

		var data [64]byte
		for j := 0; j < actualLen; j++ {
			data[j] = byte(msg.Data[j])
		}
		unifiedMsg := CanFrame{
			Direction: direction,
			ID:        id,
			DLC:       dataLenToDlc(actualLen),
			Data:      data,
			IsFD:      false,
		}

		logCANMessage("RX", unifiedMsg.ID, unifiedMsg.DLC, unifiedMsg.Data[:actualLen], CAN)
		c.publishRx(c.ctx, c.rxChan, unifiedMsg)
	}
}

func (c *Toomoss) drainInitialBuffer() {
	if c.legacyCAN {
		var canMsg [MsgBufferSize]C.CAN_MSG
		for batch := 0; batch < 16; batch++ {
			n := int(C.can_toomoss_can_get_msg(
				DevHandle[DEVIndex],
				C.uint8_t(c.CANChannel),
				&canMsg[0],
			))
			if n <= 0 {
				return
			}
		}
		log.Println("Toomoss initial classic CAN queue still contains frames after 16 batches")
		return
	}

	var canFDMsg [MsgBufferSize]C.CANFD_MSG
	for batch := 0; batch < 16; batch++ {
		n := int(C.can_toomoss_canfd_get_msg(
			DevHandle[DEVIndex],
			C.uint8_t(c.CANChannel),
			&canFDMsg[0],
			C.int(len(canFDMsg)),
		))
		if n <= 0 {
			return
		}
	}
	log.Println("Toomoss initial CAN-FD queue still contains frames after 16 batches")
}

func (c *Toomoss) Write(id int32, fd bool, data []byte) error {
	c.lifecycle.opMu.Lock()
	defer c.lifecycle.opMu.Unlock()
	if !c.lifecycle.isInitialized() {
		return errors.New("Toomoss driver is not initialized")
	}
	if err := validateWrite(c.cfg, id, fd, data); err != nil {
		return err
	}
	if c.legacyCAN {
		return c.writeClassicCAN(id, fd, data)
	}

	var msg C.CANFD_MSG
	msg.ID = C.uint32_t(id)
	msg.DLC = C.uint8_t(len(data))
	switch {
	case !fd:
		msg.Flags = C.uint8_t(CAN_MSG_FLAG_STD)
	case fd:
		msg.Flags = C.uint8_t(CANFD_MSG_FLAG_FDF)
	default:
		msg.Flags = C.uint8_t(CANFD_MSG_FLAG_FDF)
	}
	for i := 0; i < len(data) && i < 64; i++ {
		msg.Data[i] = C.uint8_t(data[i])
	}

	sendRet := int(C.can_toomoss_canfd_send_msg(
		DevHandle[DEVIndex],
		C.uint8_t(c.CANChannel),
		&msg,
		C.uint32_t(1),
	))

	if sendRet == 1 {
		logType := CAN
		if fd {
			logType = CANFD
		}

		var payload [64]byte
		copy(payload[:], data)

		unifiedMsg := CanFrame{
			Direction: TX,
			ID:        uint32(msg.ID),
			DLC:       dataLenToDlc(len(data)),
			Data:      payload,
			IsFD:      byte(msg.Flags)&CANFD_MSG_FLAG_FDF != 0,
		}

		logCANMessage("TX", uint32(id), unifiedMsg.DLC, payload[:len(data)], logType)
		if c.cfg.IncludeTxEcho {
			c.publishRx(c.ctx, c.rxChan, unifiedMsg)
		}
		return nil
	}

	log.Printf("错误: CAN/CANFD消息发送失败, ID=0x%03X", id)
	return errors.New("CAN/CANFD消息发送失败")
}

func (c *Toomoss) writeClassicCAN(id int32, fd bool, data []byte) error {
	if int(C.can_toomoss_has_can()) == 0 {
		return errors.New("CAN_SendMsg not loaded")
	}
	if fd {
		return errors.New("legacy Toomoss firmware does not support CAN-FD frames")
	}
	if len(data) > 8 {
		return fmt.Errorf("data length %d exceeds CAN maximum length 8", len(data))
	}

	var canMsg C.CAN_MSG
	canID := uint32(id) & toomossCANFDIDMaskStandard
	canMsg.ID = C.uint32_t(canID)
	remoteFlag, externFlag := encodeToomossClassicFlags(c.CANChannel, false, false)
	canMsg.RemoteFlag = C.uint8_t(remoteFlag)
	canMsg.ExternFlag = C.uint8_t(externFlag)
	canMsg.DataLen = C.uint8_t(len(data))
	for i := range data {
		canMsg.Data[i] = C.uint8_t(data[i])
	}

	sendRet := int(C.can_toomoss_can_send_msg(
		DevHandle[DEVIndex],
		C.uint8_t(c.CANChannel),
		&canMsg,
		C.uint32_t(1),
	))
	if sendRet != 1 {
		log.Printf("error: CAN message send failed, ID=0x%03X", canID)
		return errors.New("CAN message send failed")
	}

	var unifiedData [64]byte
	copy(unifiedData[:], data)
	unifiedMsg := CanFrame{
		Direction: TX,
		ID:        canID,
		DLC:       dataLenToDlc(len(data)),
		Data:      unifiedData,
		IsFD:      false,
	}
	logCANMessage("TX", canID, unifiedMsg.DLC, data, CAN)
	if c.cfg.IncludeTxEcho {
		c.publishRx(c.ctx, c.rxChan, unifiedMsg)
	}
	return nil
}

func (c *Toomoss) RxChan() <-chan CanFrame {
	c.lifecycle.opMu.Lock()
	defer c.lifecycle.opMu.Unlock()
	if c.fanout == nil {
		return nil
	}
	ch, _ := c.fanout.Subscribe(c.cfg.RxBufferSize)
	return ch
}

func (c *Toomoss) SubscribeRx(buffer int) (<-chan CanFrame, func()) {
	c.lifecycle.opMu.Lock()
	defer c.lifecycle.opMu.Unlock()
	if c.fanout == nil {
		return nil, func() {}
	}
	return c.fanout.Subscribe(buffer)
}

func (c *Toomoss) IsFDMode() bool {
	c.lifecycle.opMu.Lock()
	defer c.lifecycle.opMu.Unlock()
	return c.canType == CANFD
}

func (c *Toomoss) Config() Config {
	c.lifecycle.opMu.Lock()
	defer c.lifecycle.opMu.Unlock()
	return c.cfg
}
