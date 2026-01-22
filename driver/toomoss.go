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
	CANFDInit      uintptr

	CANFDStartGetMsg     uintptr
	CANFD_GetMsg         uintptr
	CANFD_SendMsg        uintptr
	CANFD_GetCANSpeedArg uintptr

	DevHandle [10]int
	DEVIndex  = 0

	toomossMu sync.Mutex
)

func toomossReady() bool {
	return UsbDeviceDLL != 0 &&
		UsbScanDevice != 0 &&
		UsbOpenDevice != 0 &&
		UsbCloseDevice != 0 &&
		CANFDInit != 0 &&
		CANFDStartGetMsg != 0 &&
		CANFD_GetMsg != 0 &&
		CANFD_SendMsg != 0 &&
		CANFD_GetCANSpeedArg != 0
}

func resetToomossState() {
	UsbDeviceDLL = 0
	UsbScanDevice = 0
	UsbOpenDevice = 0
	UsbCloseDevice = 0
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

func archDLLDir() string {
	if runtime.GOARCH == "386" {
		return "windows_x86"
	}
	return "windows_x64"
}

func loadDLLs() error {
	if UsbDeviceDLL != 0 {
		return nil
	}

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

	dllDir := archDLLDir()
	libusbPath := filepath.Join(".\\DLLs", dllDir, "libusb-1.0.dll")
	if _, err := syscall.LoadLibrary(libusbPath); err != nil {
		log.Printf("Warning: failed to load libusb-1.0.dll from %s: %v", libusbPath, err)
	}

	usbPath := filepath.Join(".\\DLLs", dllDir, "USB2XXX.dll")
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
	if CANFDInit, err = getProc("CANFD_Init"); err != nil {
		errs = append(errs, err.Error())
	}
	if CANFDStartGetMsg, err = getProc("CANFD_StartGetMsg"); err != nil {
		errs = append(errs, err.Error())
	}
	if CANFD_GetMsg, err = getProc("CANFD_GetMsg"); err != nil {
		errs = append(errs, err.Error())
	}
	if CANFD_SendMsg, err = getProc("CANFD_SendMsg"); err != nil {
		errs = append(errs, err.Error())
	}
	if CANFD_GetCANSpeedArg, err = getProc("CANFD_GetCANSpeedArg"); err != nil {
		errs = append(errs, err.Error())
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
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
	defer k.Close()

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
		sk.Close()

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
		sk.Close()

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

type CanMix struct {
	rxChan  chan UnifiedCANMessage
	ctx     context.Context
	cancel  context.CancelFunc
	canType CanType
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
	if err := ensureToomossLoaded(); err != nil {
		return fmt.Errorf("failed to load Toomoss DLLs: %w", err)
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
	fdSpeed, _, callErr := syscall.SyscallN(
		CANFD_GetCANSpeedArg,
		uintptr(DevHandle[DEVIndex]),
		uintptr(unsafe.Pointer(&canFDInitConfig)),
		uintptr(SpeedBpsNBT),
		uintptr(SpeedBpsDBT),
	)
	if callErr != 0 {
		return fmt.Errorf("CANFD_GetCANSpeedArg syscall failed: %w", callErr)
	}
	canfdInit, _, callErr := syscall.SyscallN(
		CANFDInit,
		uintptr(DevHandle[DEVIndex]),
		uintptr(CanChannel),
		uintptr(unsafe.Pointer(&canFDInitConfig)),
	)
	if callErr != 0 {
		return fmt.Errorf("CANFD_Init syscall failed: %w", callErr)
	}
	fdStart, _, callErr := syscall.SyscallN(
		CANFDStartGetMsg,
		uintptr(DevHandle[DEVIndex]),
		uintptr(CanChannel),
	)
	if callErr != 0 {
		return fmt.Errorf("CANFD_StartGetMsg syscall failed: %w", callErr)
	}
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
	c.drainInitialBuffer()
	go c.readLoop()
}

func (c *CanMix) Stop() {
	log.Println("正在停止CAN-FD驱动的读取服务...")
	c.cancel()
	if err := usbClose(); err != nil {
		log.Printf("警告: USB关闭失败: %v", err)
	}
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
				actualLen := dlcToLen(msg.DLC)
				if actualLen == 0 {
					continue
				}
				unifiedMsg := UnifiedCANMessage{
					ID: msg.ID, DLC: msg.DLC, Data: msg.Data, IsFD: msg.Flags == CANFD_MSG_FLAG_FDF,
				}

				msgType := c.canType
				if msg.Flags == CAN_MSG_FLAG_STD {
					msgType = CAN
				} else {
					msgType = CANFD
				}
				logCANMessage("RX", unifiedMsg.ID, dataLenToDlc(int(unifiedMsg.DLC)), unifiedMsg.Data[:msg.DLC], msgType)

				select {
				case c.rxChan <- unifiedMsg:
				default:
					log.Println("警告: 驱动接收channel(FD)已满，消息被丢弃")
				}
			}
		}
	}
}

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
	sendRet, _, _ := syscall.SyscallN(
		CANFD_SendMsg,
		uintptr(DevHandle[DEVIndex]),
		uintptr(CanChannel),
		uintptr(unsafe.Pointer(&canFDMsg[0])),
		uintptr(len(canFDMsg)),
	)

	if int(sendRet) == len(canFDMsg) {
	    tempTmsDlc:=8
	    if tmsDlc:= dataLenToDlc(len(data));tmsDlc>=8{
	        tempTmsDlc = tmsDlc
	    }
		logCANMessage("TX", uint32(id), tempTmsDlc, canFDMsg[0].Data[:canFDMsg[0].DLC], c.canType)
	} else {
		log.Printf("错误: CAN/CANFD消息发送失败, ID=0x%03X", id)
		return errors.New("CAN/CANFD消息发送失败")
	}
	return nil
}

func (c *CanMix) RxChan() <-chan UnifiedCANMessage { return c.rxChan }

func (c *CanMix) Context() context.Context { return c.ctx }
