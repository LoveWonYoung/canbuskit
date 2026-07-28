//go:build windows

package driver

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

var (
	UsbDeviceDLL syscall.Handle

	UsbScanDevice  uintptr
	UsbOpenDevice  uintptr
	UsbCloseDevice uintptr

	CANInit           uintptr
	CANStartGetMsg    uintptr
	CANGetMsg         uintptr
	CANSendMsg        uintptr
	CANGetCANSpeedArg uintptr

	CANFDInit uintptr

	CANFDStartGetMsg     uintptr
	CANFD_GetMsg         uintptr
	CANFD_SendMsg        uintptr
	CANFD_GetCANSpeedArg uintptr

	DevHandle [10]int
	DEVIndex  = 0

	toomossMu        sync.Mutex
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

func toomossReady() bool {
	return UsbDeviceDLL != 0 &&
		UsbScanDevice != 0 &&
		UsbOpenDevice != 0 &&
		UsbCloseDevice != 0 &&
		((CANInit != 0 &&
			CANStartGetMsg != 0 &&
			CANGetMsg != 0 &&
			CANSendMsg != 0 &&
			CANGetCANSpeedArg != 0) ||
			(CANFDInit != 0 &&
				CANFDStartGetMsg != 0 &&
				CANFD_GetMsg != 0 &&
				CANFD_SendMsg != 0 &&
				CANFD_GetCANSpeedArg != 0))
}

func resetToomossState() {
	UsbDeviceDLL = 0
	UsbScanDevice = 0
	UsbOpenDevice = 0
	UsbCloseDevice = 0
	CANInit = 0
	CANStartGetMsg = 0
	CANGetMsg = 0
	CANSendMsg = 0
	CANGetCANSpeedArg = 0
	CANFDInit = 0
	CANFDStartGetMsg = 0
	CANFD_GetMsg = 0
	CANFD_SendMsg = 0
	CANFD_GetCANSpeedArg = 0
}

func ensureToomossLoaded() error {
	toomossMu.Lock()
	defer toomossMu.Unlock()

	if toomossReady() {
		return nil
	}

	resetToomossState()

	if err := loadDLLs(); err != nil {
		return err
	}

	if err := loadProcAddresses(); err != nil {
		if UsbDeviceDLL != 0 {
			_ = syscall.FreeLibrary(UsbDeviceDLL)
		}
		resetToomossState()
		return err
	}

	return nil
}

func loadDLLs() error {
	if UsbDeviceDLL != 0 {
		return nil
	}

	if runtime.GOARCH == "386" {
		if registryPath := getRegistryPath(); registryPath != "" {
			fmt.Println("Found registry path:", registryPath)
			libusbPath := filepath.Join(registryPath, "libusb-1.0.dll")
			if _, err := syscall.LoadLibrary(libusbPath); err != nil {
				fmt.Println("Warning: Failed to load libusb-1.0.dll from", libusbPath, "Error:", err)
			}

			usbPath := filepath.Join(registryPath, "USB2XXX.dll")
			if handle, err := syscall.LoadLibrary(usbPath); err == nil {
				UsbDeviceDLL = handle
				fmt.Println("Loaded DLLs from registry path:", registryPath)
				return nil
			} else {
				fmt.Println("Failed to load USB2XXX.dll from", usbPath, "Error:", err)
			}
		} else {
			fmt.Println("Registry path not found")
		}
	}

	libusbPath := filepath.Join(".\\bin", "libusb-1.0.dll")
	if _, err := syscall.LoadLibrary(libusbPath); err != nil {
		log.Printf("Warning: failed to load libusb-1.0.dll from %s: %v", libusbPath, err)
	}

	usbPath := filepath.Join(".\\bin", "USB2XXX.dll")
	handle, err := syscall.LoadLibrary(usbPath)
	if err != nil {
		return fmt.Errorf("failed to load USB2XXX.dll from %s: %w", usbPath, err)
	}
	UsbDeviceDLL = handle
	log.Printf("Loaded DLLs from default path: %s", usbPath)
	return nil
}

func getProc(name string) (uintptr, error) {
	addr, err := syscall.GetProcAddress(UsbDeviceDLL, name)
	if addr == 0 {
		if err == nil {
			err = errors.New("not found")
		}
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return addr, nil
}

func loadProcAddresses() error {
	if UsbDeviceDLL == 0 {
		return errors.New("USB2XXX.dll not loaded")
	}

	var errs []string
	var err error

	if UsbScanDevice, err = getProc("USB_ScanDevice"); err != nil {
		errs = append(errs, err.Error())
	}
	if UsbOpenDevice, err = getProc("USB_OpenDevice"); err != nil {
		errs = append(errs, err.Error())
	}
	if UsbCloseDevice, err = getProc("USB_CloseDevice"); err != nil {
		errs = append(errs, err.Error())
	}
	loadOptionalProc("CAN_Init", &CANInit)
	loadOptionalProc("CAN_StartGetMsg", &CANStartGetMsg)
	loadOptionalProc("CAN_GetMsg", &CANGetMsg)
	loadOptionalProc("CAN_SendMsg", &CANSendMsg)
	loadOptionalProc("CAN_GetCANSpeedArg", &CANGetCANSpeedArg)

	loadOptionalProc("CANFD_Init", &CANFDInit)
	loadOptionalProc("CANFD_StartGetMsg", &CANFDStartGetMsg)
	loadOptionalProc("CANFD_GetMsg", &CANFD_GetMsg)
	loadOptionalProc("CANFD_SendMsg", &CANFD_SendMsg)
	loadOptionalProc("CANFD_GetCANSpeedArg", &CANFD_GetCANSpeedArg)

	if len(errs) > 0 && !toomossReady() {
		return errors.New(strings.Join(errs, "; "))
	}
	if !toomossReady() {
		return errors.New("required Toomoss CAN procedures are not available")
	}
	return nil
}

func loadOptionalProc(name string, dest *uintptr) {
	addr, err := getProc(name)
	if err != nil {
		log.Printf("Toomoss proc not available: %s (%v)", name, err)
		*dest = 0
		return
	}
	*dest = addr
}

func getRegistryPath() string {
	const uninstall = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`

	views := []struct {
		label  string
		access uint32
	}{
		{"64", registry.READ | registry.WOW64_64KEY},
		{"32", registry.READ | registry.WOW64_32KEY},
		{"default", registry.READ},
	}

	for _, view := range views {
		if path := findRegistryPathInView(uninstall, view.label, view.access); path != "" {
			return path
		}
	}

	return ""
}

func dirFromUninstallString(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.Trim(s, `"`)
	if i := strings.IndexByte(s, ' '); i > 0 {
		s = s[:i]
	}
	s = strings.Trim(s, `"`)
	if s == "" {
		return ""
	}
	return filepath.Dir(s)
}

func findRegistryPathInView(uninstall, label string, access uint32) string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, uninstall, access)
	if err != nil {
		fmt.Println("OpenKey HKLM", label, "view failed:", err)
		return ""
	}
	defer func(k registry.Key) {
		err := k.Close()
		if err != nil {

		}
	}(k)

	names, err := k.ReadSubKeyNames(-1)
	if err != nil {
		fmt.Println("ReadSubKeyNames failed:", err)
		return ""
	}

	fmt.Println("HKLM", label, "view entries:", len(names))

	for _, name := range names {
		sk, err := registry.OpenKey(registry.LOCAL_MACHINE, uninstall+`\`+name, access)
		if err != nil {
			continue
		}

		publisher, _, _ := sk.GetStringValue("Publisher")
		displayName, _, _ := sk.GetStringValue("DisplayName")
		install, _, _ := sk.GetStringValue("InstallLocation")
		appPath, _, _ := sk.GetStringValue("Inno Setup: App Path")
		unins, _, _ := sk.GetStringValue("UninstallString")
		err = sk.Close()
		if err != nil {
			return ""
		}

		pubL := strings.ToLower(strings.TrimSpace(publisher))
		dnL := strings.ToLower(strings.TrimSpace(displayName))

		if strings.Contains(pubL, "toomoss") || strings.Contains(dnL, "toomoss") {
			fmt.Println("Matched subkey:", name)
			fmt.Println("  DisplayName:", displayName)
			fmt.Println("  Publisher:", publisher)

			install = strings.TrimSpace(install)
			if install != "" {
				fmt.Println("  InstallLocation:", install)
				return filepath.Clean(install)
			}

			appPath = strings.TrimSpace(appPath)
			if appPath != "" {
				fmt.Println("  AppPath:", appPath)
				return filepath.Clean(appPath)
			}

			if dir := dirFromUninstallString(unins); dir != "" {
				fmt.Println("  From UninstallString:", dir)
				if hasUSB2XXXDLL(dir) {
					return dir
				}
				fmt.Println("  UninstallString path missing USB2XXX.dll")
			}

			fmt.Println("  No usable path fields")
		}
	}

	for _, name := range names {
		sk, err := registry.OpenKey(registry.LOCAL_MACHINE, uninstall+`\`+name, access)
		if err != nil {
			continue
		}

		install, _, _ := sk.GetStringValue("InstallLocation")
		appPath, _, _ := sk.GetStringValue("Inno Setup: App Path")
		unins, _, _ := sk.GetStringValue("UninstallString")
		err = sk.Close()
		if err != nil {
			return ""
		}

		install = strings.TrimSpace(install)
		if install != "" && pathLooksToomoss(install) {
			fmt.Println("Matched InstallLocation by path hint:", name)
			return filepath.Clean(install)
		}

		appPath = strings.TrimSpace(appPath)
		if appPath != "" && pathLooksToomoss(appPath) {
			fmt.Println("Matched AppPath by path hint:", name)
			return filepath.Clean(appPath)
		}

		if dir := dirFromUninstallString(unins); dir != "" && pathLooksToomoss(dir) {
			fmt.Println("Matched UninstallString by path hint:", name)
			return dir
		}
	}

	return ""
}

func hasUSB2XXXDLL(dir string) bool {
	if dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, "USB2XXX.dll"))
	return err == nil
}

func pathLooksToomoss(p string) bool {
	pl := strings.ToLower(p)
	return strings.Contains(pl, "toomoss") || strings.Contains(pl, "tcanlinpro")
}

func usbScan() (bool, error) {
	if UsbScanDevice == 0 {
		return false, errors.New("USB_ScanDevice not loaded")
	}
	ret, _, callErr := syscall.SyscallN(
		UsbScanDevice,
		uintptr(unsafe.Pointer(&DevHandle[DEVIndex])),
	)
	if callErr != 0 {
		return false, fmt.Errorf("USB_ScanDevice syscall failed: %w", callErr)
	}
	return ret > 0, nil
}

func UsbScan() bool {
	if err := ensureToomossLoaded(); err != nil {
		log.Printf("USB scan failed (load DLLs): %v", err)
		return false
	}
	ok, err := usbScan()
	if err != nil {
		log.Printf("USB scan failed: %v", err)
		return false
	}
	return ok
}

func usbOpen() (bool, error) {
	if UsbOpenDevice == 0 {
		return false, errors.New("USB_OpenDevice not loaded")
	}
	stateValue, _, callErr := syscall.SyscallN(
		UsbOpenDevice,
		uintptr(DevHandle[DEVIndex]),
	)
	if callErr != 0 {
		return false, fmt.Errorf("USB_OpenDevice syscall failed: %w", callErr)
	}
	return stateValue >= 1, nil
}

func UsbOpen() bool {
	if err := ensureToomossLoaded(); err != nil {
		log.Printf("USB open failed (load DLLs): %v", err)
		return false
	}
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

	if UsbDeviceDLL == 0 {
		return nil
	}
	if UsbCloseDevice == 0 {
		return errors.New("USB_CloseDevice not loaded")
	}
	ret, _, callErr := syscall.SyscallN(
		UsbCloseDevice,
		uintptr(DevHandle[DEVIndex]),
	)
	if callErr != 0 {
		return fmt.Errorf("USB_CloseDevice syscall failed: %w", callErr)
	}
	if ret < 1 {
		return fmt.Errorf("USB_CloseDevice returned %d", ret)
	}
	if err := syscall.FreeLibrary(UsbDeviceDLL); err != nil {
		return fmt.Errorf("FreeLibrary failed: %w", err)
	}
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

type CAN_INIT_CONFIG struct {
	CAN_BRP  uint
	CAN_SJW  byte
	CAN_BS1  byte
	CAN_BS2  byte
	CAN_Mode byte
	CAN_ABOM byte
	CAN_NART byte
	CAN_RFLM byte
	CAN_TXFP byte
}

type CAN_MSG struct {
	ID            int32
	TimeStamp     int32
	RemoteFlag    byte
	ExternFlag    byte
	DataLen       byte
	Data          [8]byte
	TimeStampHigh byte
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

const (
	//	c.CANChannel  = 0
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
	CANFD_MSG_FLAG_BRS = 1 << (iota - 1) // CANFD加速帧标志
	CANFD_MSG_FLAG_ESI                   // CANFD错误状态指示
	CANFD_MSG_FLAG_FDF                   // CANFD帧标志
)

const (
	toomossCANFDIDMaskStandard = 0x7FF
	toomossCANFDIDMaskExtended = 0x1FFFFFFF
	toomossClassicFlagRemote   = 0x01
	toomossClassicFlagChannel  = 0x60
	toomossClassicFlagTx       = 0x80
	toomossClassicFlagExt      = 0x01
	toomossClassicFlagError    = 0x80
)

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
		return cleanup(fmt.Errorf("failed to load Toomoss DLLs: %w", err))
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
	if CANFD_GetCANSpeedArg == 0 || CANFDInit == 0 || CANFDStartGetMsg == 0 || CANFD_GetMsg == 0 || CANFD_SendMsg == 0 {
		return fallback(errors.New("CAN-FD APIs are not available in USB2XXX.dll"))
	}

	canFDInitConfig := c.canFDInitConfig
	fdSpeed, _, callErr := syscall.SyscallN(
		CANFD_GetCANSpeedArg,
		uintptr(DevHandle[DEVIndex]),
		uintptr(unsafe.Pointer(&canFDInitConfig)),
		uintptr(c.cfg.NominalBitrate),
		uintptr(c.cfg.DataBitrate),
	)
	if callErr != 0 {
		return fallback(fmt.Errorf("CANFD_GetCANSpeedArg syscall failed: %w", callErr))
	}
	canfdInit, _, callErr := syscall.SyscallN(
		CANFDInit,
		uintptr(DevHandle[DEVIndex]),
		uintptr(c.CANChannel),
		uintptr(unsafe.Pointer(&canFDInitConfig)),
	)
	if callErr != 0 {
		return fallback(fmt.Errorf("CANFD_Init syscall failed: %w", callErr))
	}
	fdStart, _, callErr := syscall.SyscallN(
		CANFDStartGetMsg,
		uintptr(DevHandle[DEVIndex]),
		uintptr(c.CANChannel),
	)
	if callErr != 0 {
		return fallback(fmt.Errorf("CANFD_StartGetMsg syscall failed: %w", callErr))
	}
	time.Sleep(InitDelay)
	if !(canfdInit == 0 && fdStart == 0 && fdSpeed == 0) {
		return fallback(fmt.Errorf("CAN-FD initialization failed: CANFD_Init=%d, CANFD_StartGetMsg=%d, CANFD_GetCANSpeedArg=%d", canfdInit, fdStart, fdSpeed))
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
	if CANGetCANSpeedArg == 0 || CANInit == 0 || CANStartGetMsg == 0 {
		return errors.New("standard CAN APIs are not available in USB2XXX.dll")
	}

	canInitConfig := CAN_INIT_CONFIG{
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

	ret, _, callErr := syscall.SyscallN(
		CANGetCANSpeedArg,
		uintptr(DevHandle[DEVIndex]),
		uintptr(unsafe.Pointer(&canInitConfig)),
		uintptr(c.cfg.NominalBitrate),
	)
	if callErr != 0 {
		return fmt.Errorf("CAN_GetCANSpeedArg syscall failed: %w", callErr)
	}
	if ret != CAN_SUCCESS {
		return fmt.Errorf("CAN_GetCANSpeedArg returned %d", ret)
	}

	canInitRet, _, callErr := syscall.SyscallN(
		CANInit,
		uintptr(DevHandle[DEVIndex]),
		uintptr(c.CANChannel),
		uintptr(unsafe.Pointer(&canInitConfig)),
	)
	if callErr != 0 {
		return fmt.Errorf("CAN_Init syscall failed: %w", callErr)
	}
	startRet, _, callErr := syscall.SyscallN(
		CANStartGetMsg,
		uintptr(DevHandle[DEVIndex]),
		uintptr(c.CANChannel),
	)
	if callErr != 0 {
		return fmt.Errorf("CAN_StartGetMsg syscall failed: %w", callErr)
	}
	time.Sleep(InitDelay)
	if canInitRet != CAN_SUCCESS || startRet != CAN_SUCCESS {
		return fmt.Errorf("standard CAN initialization failed: CAN_Init=%d, CAN_StartGetMsg=%d", canInitRet, startRet)
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
	var canMsg [MsgBufferSize]CAN_MSG
	var canFDMsg [MsgBufferSize]CANFD_MSG
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if c.legacyCAN {
				c.readClassicBurst(&canMsg)
				continue
			}
			getCanFDMsgNum, _, _ := syscall.SyscallN(
				CANFD_GetMsg,
				uintptr(DevHandle[DEVIndex]),
				uintptr(c.CANChannel),
				uintptr(unsafe.Pointer(&canFDMsg[0])),
				uintptr(len(canFDMsg)),
			)

			if getCanFDMsgNum <= 0 {
				continue
			}

			for i := 0; i < int(getCanFDMsgNum); i++ {
				msg := canFDMsg[i]
				if msg.ID > toomossCANFDIDMaskStandard {
					continue
				}
				isFD := msg.Flags&CANFD_MSG_FLAG_FDF != 0
				actualLen := toomossDLCToDataLen(msg.DLC, isFD)
				normalizedDLC := dataLenToDlc(actualLen)
				unifiedMsg := CanFrame{
					Direction: RX, ID: msg.ID, DLC: normalizedDLC, Data: msg.Data, IsFD: isFD,
				}

				msgType := c.canType
				if msg.Flags == CAN_MSG_FLAG_STD {
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

func (c *Toomoss) readClassicBurst(canMsg *[MsgBufferSize]CAN_MSG) {
	if CANGetMsg == 0 {
		log.Println("CAN_GetMsg not loaded")
		return
	}

	getCANMsgNum, _, _ := syscall.SyscallN(
		CANGetMsg,
		uintptr(DevHandle[DEVIndex]),
		uintptr(c.CANChannel),
		uintptr(unsafe.Pointer(&canMsg[0])),
	)
	if getCANMsgNum <= 0 {
		return
	}

	for i := 0; i < int(getCANMsgNum); i++ {
		msg := canMsg[i]
		_, remote, extended, errorFrame, txEcho := decodeToomossClassicFlags(msg.RemoteFlag, msg.ExternFlag)
		if errorFrame {
			continue
		}
		if extended {
			continue
		}
		if remote {
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
		if actualLen > 0 {
			copy(data[:], msg.Data[:actualLen])
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
		var canMsg [MsgBufferSize]CAN_MSG
		for batch := 0; batch < 16; batch++ {
			n, _, _ := syscall.SyscallN(
				CANGetMsg,
				uintptr(DevHandle[DEVIndex]),
				uintptr(c.CANChannel),
				uintptr(unsafe.Pointer(&canMsg[0])),
			)
			if int(n) <= 0 {
				return
			}
		}
		log.Println("Toomoss initial classic CAN queue still contains frames after 16 batches")
		return
	}

	var canFDMsg [MsgBufferSize]CANFD_MSG
	for batch := 0; batch < 16; batch++ {
		n, _, _ := syscall.SyscallN(
			CANFD_GetMsg,
			uintptr(DevHandle[DEVIndex]),
			uintptr(c.CANChannel),
			uintptr(unsafe.Pointer(&canFDMsg[0])),
			uintptr(len(canFDMsg)),
		)
		if int(n) <= 0 {
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

	var canFDMsg [1]CANFD_MSG
	var tempData [64]byte
	copy(tempData[:], data)
	canFDMsg[0].ID = uint32(id)
	switch {
	case !fd:
		canFDMsg[0].Flags = 0
	case fd:
		canFDMsg[0].Flags = CANFD_MSG_FLAG_FDF
	default:
		canFDMsg[0].Flags = CANFD_MSG_FLAG_FDF
	}

	canFDMsg[0].DLC = byte(len(data))
	canFDMsg[0].Data = tempData

	sendRet, _, _ := syscall.SyscallN(
		CANFD_SendMsg,
		uintptr(DevHandle[DEVIndex]),
		uintptr(c.CANChannel),
		uintptr(unsafe.Pointer(&canFDMsg[0])),
		uintptr(len(canFDMsg)),
	)

	if int(sendRet) == len(canFDMsg) {
		logType := CAN
		if fd {
			logType = CANFD
		}

		normalizedDLC := dataLenToDlc(len(data))
		unifiedMsg := CanFrame{
			Direction: TX, ID: canFDMsg[0].ID, DLC: normalizedDLC, Data: canFDMsg[0].Data, IsFD: canFDMsg[0].Flags&CANFD_MSG_FLAG_FDF != 0,
		}

		logCANMessage("TX", uint32(id), normalizedDLC, canFDMsg[0].Data[:len(data)], logType)
		if c.cfg.IncludeTxEcho {
			c.publishRx(c.ctx, c.rxChan, unifiedMsg)
		}
	} else {
		log.Printf("错误: CAN/CANFD消息发送失败, ID=0x%03X", id)
		return errors.New("CAN/CANFD消息发送失败")
	}
	return nil
}

func (c *Toomoss) writeClassicCAN(id int32, fd bool, data []byte) error {
	if CANSendMsg == 0 {
		return errors.New("CAN_SendMsg not loaded")
	}
	if fd {
		return errors.New("legacy Toomoss firmware does not support CAN-FD frames")
	}
	if len(data) > 8 {
		return fmt.Errorf("data length %d exceeds CAN maximum length 8", len(data))
	}

	var canMsg CAN_MSG
	copy(canMsg.Data[:], data)
	canID := uint32(id) & toomossCANFDIDMaskStandard
	canMsg.ID = int32(canID)
	remoteFlag, externFlag := encodeToomossClassicFlags(c.CANChannel, false, false)
	canMsg.RemoteFlag = remoteFlag
	canMsg.ExternFlag = externFlag
	canMsg.DataLen = byte(len(data))

	sendRet, _, _ := syscall.SyscallN(
		CANSendMsg,
		uintptr(DevHandle[DEVIndex]),
		uintptr(c.CANChannel),
		uintptr(unsafe.Pointer(&canMsg)),
		uintptr(1),
	)
	if int(sendRet) != 1 {
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
