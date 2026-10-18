//go:build windows

package backend

import (
	"context"
	"errors"
	"fmt"
	"github.com/LoveWonYoung/canbuskit/v2/can"
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
	toomossSessionMu sync.Mutex
	toomossInUse     bool
)

type toomossAPI struct {
	dll      syscall.Handle
	scan     uintptr
	open     uintptr
	close    uintptr
	canInit  uintptr
	canStart uintptr
	canGet   uintptr
	canSend  uintptr
	canSpeed uintptr
	fdInit   uintptr
	fdStart  uintptr
	fdGet    uintptr
	fdSend   uintptr
	fdSpeed  uintptr
}

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

func (api toomossAPI) ready() bool {
	return api.dll != 0 && api.scan != 0 && api.open != 0 && api.close != 0 &&
		((api.canInit != 0 && api.canStart != 0 && api.canGet != 0 && api.canSend != 0 && api.canSpeed != 0) ||
			(api.fdInit != 0 && api.fdStart != 0 && api.fdGet != 0 && api.fdSend != 0 && api.fdSpeed != 0))
}

func (api *toomossAPI) release() {
	if api == nil || api.dll == 0 {
		return
	}
	_ = syscall.FreeLibrary(api.dll)
	*api = toomossAPI{}
}

func loadToomossAPI() (toomossAPI, error) {
	var api toomossAPI
	if err := api.loadDLL(); err != nil {
		return toomossAPI{}, err
	}
	if err := api.loadProcs(); err != nil {
		api.release()
		return toomossAPI{}, err
	}
	return api, nil
}

func (api *toomossAPI) loadDLL() error {
	if runtime.GOARCH == "386" {
		if registryPath := getRegistryPath(); registryPath != "" {
			libusbPath := filepath.Join(registryPath, "libusb-1.0.dll")
			_, _ = syscall.LoadLibrary(libusbPath)
			usbPath := filepath.Join(registryPath, "USB2XXX.dll")
			if handle, err := syscall.LoadLibrary(usbPath); err == nil {
				api.dll = handle
				return nil
			}
		}
	}
	libusbPath := filepath.Join(".\\bin", "libusb-1.0.dll")
	_, _ = syscall.LoadLibrary(libusbPath)
	usbPath := filepath.Join(".\\bin", "USB2XXX.dll")
	handle, err := syscall.LoadLibrary(usbPath)
	if err != nil {
		return fmt.Errorf("failed to load USB2XXX.dll from %s: %w", usbPath, err)
	}
	api.dll = handle
	return nil
}

func (api *toomossAPI) proc(name string) (uintptr, error) {
	addr, err := syscall.GetProcAddress(api.dll, name)
	if addr == 0 {
		if err == nil {
			err = errors.New("not found")
		}
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return addr, nil
}

func (api *toomossAPI) loadProcs() error {
	if api.dll == 0 {
		return errors.New("USB2XXX.dll not loaded")
	}
	var errs []string
	var err error
	if api.scan, err = api.proc("USB_ScanDevice"); err != nil {
		errs = append(errs, err.Error())
	}
	if api.open, err = api.proc("USB_OpenDevice"); err != nil {
		errs = append(errs, err.Error())
	}
	if api.close, err = api.proc("USB_CloseDevice"); err != nil {
		errs = append(errs, err.Error())
	}
	api.canInit, _ = api.proc("CAN_Init")
	api.canStart, _ = api.proc("CAN_StartGetMsg")
	api.canGet, _ = api.proc("CAN_GetMsg")
	api.canSend, _ = api.proc("CAN_SendMsgWithTime")
	api.canSpeed, _ = api.proc("CAN_GetCANSpeedArg")
	api.fdInit, _ = api.proc("CANFD_Init")
	api.fdStart, _ = api.proc("CANFD_StartGetMsg")
	api.fdGet, _ = api.proc("CANFD_GetMsg")
	api.fdSend, _ = api.proc("CANFD_SendMsgWithTime")
	api.fdSpeed, _ = api.proc("CANFD_GetCANSpeedArg")
	if len(errs) > 0 && !api.ready() {
		return errors.New(strings.Join(errs, "; "))
	}
	if !api.ready() {
		return errors.New("required Toomoss CAN procedures are not available")
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

func findRegistryPathInView(uninstall, _ string, access uint32) string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, uninstall, access)
	if err != nil {
		return ""
	}
	defer func(k registry.Key) {
		err := k.Close()
		if err != nil {

		}
	}(k)

	names, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return ""
	}

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
			install = strings.TrimSpace(install)
			if install != "" {
				return filepath.Clean(install)
			}

			appPath = strings.TrimSpace(appPath)
			if appPath != "" {
				return filepath.Clean(appPath)
			}

			if dir := dirFromUninstallString(unins); dir != "" {
				if hasUSB2XXXDLL(dir) {
					return dir
				}
			}
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
			return filepath.Clean(install)
		}

		appPath = strings.TrimSpace(appPath)
		if appPath != "" && pathLooksToomoss(appPath) {
			return filepath.Clean(appPath)
		}

		if dir := dirFromUninstallString(unins); dir != "" && pathLooksToomoss(dir) {
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

func (t *Toomoss) scan() (bool, error) {
	if t.api.scan == 0 {
		return false, errors.New("USB_ScanDevice not loaded")
	}
	ret, _, callErr := syscall.SyscallN(
		t.api.scan,
		uintptr(unsafe.Pointer(&t.handle)),
	)
	if callErr != 0 {
		return false, fmt.Errorf("USB_ScanDevice syscall failed: %w", callErr)
	}
	return ret > 0, nil
}

func (t *Toomoss) openUSB() (bool, error) {
	if t.api.open == 0 {
		return false, errors.New("USB_OpenDevice not loaded")
	}
	stateValue, _, callErr := syscall.SyscallN(
		t.api.open,
		uintptr(t.handle),
	)
	if callErr != 0 {
		return false, fmt.Errorf("USB_OpenDevice syscall failed: %w", callErr)
	}
	return stateValue >= 1, nil
}

func (t *Toomoss) closeUSB() error {
	if t.api.dll == 0 {
		return nil
	}
	if t.api.close == 0 {
		t.api.release()
		return errors.New("USB_CloseDevice not loaded")
	}
	ret, _, callErr := syscall.SyscallN(
		t.api.close,
		uintptr(t.handle),
	)
	t.api.release()
	if callErr != 0 {
		return fmt.Errorf("USB_CloseDevice syscall failed: %w", callErr)
	}
	if ret < 1 {
		return fmt.Errorf("USB_CloseDevice returned %d", ret)
	}
	return nil
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
	TimeStamp     uint32
	RemoteFlag    byte
	ExternFlag    byte
	DataLen       byte
	Data          [8]byte
	TimeStampHigh byte
}

type CANFD_MSG struct {
	ID            uint32
	DLC           byte
	Flags         byte
	__Res0        byte
	TimeStampHigh byte
	TimeStamp     uint32
	Data          [64]byte
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
	CANFD_MSG_FLAG_BRS = 1 << (iota - 1) // CAN FD bit-rate switch
	CANFD_MSG_FLAG_ESI                   // CAN FD error state indicator
	CANFD_MSG_FLAG_FDF                   // CAN FD frame format
)

const (
	toomossCANFDIDMaskStandard = 0x7FF
	toomossCANFDIDMaskExtended = 0x1FFFFFFF
	toomossCANFDIDFlagRemote   = 1 << 30
	toomossCANFDIDFlagExtended = 1 << 31
	toomossCANFDFlagChannel    = 0x60
	toomossCANFDFlagTx         = 0x80
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

// applyToomossTiming copies explicit phases onto Toomoss defaults.
func applyToomossTiming(cfg can.Config) CANFD_INIT_CONFIG {
	hw := defaultCANFDInitConfig()
	hw.NBT_BRP = cfg.NominalTiming.BRP
	hw.NBT_SEG1 = cfg.NominalTiming.SEG1
	hw.NBT_SEG2 = cfg.NominalTiming.SEG2
	hw.NBT_SJW = cfg.NominalTiming.SJW
	hw.DBT_BRP = cfg.DataTiming.BRP
	hw.DBT_SEG1 = cfg.DataTiming.SEG1
	hw.DBT_SEG2 = cfg.DataTiming.SEG2
	hw.DBT_SJW = cfg.DataTiming.SJW
	return hw
}

type Toomoss struct {
	observability
	rxChan     chan can.Frame
	fanout     *rxFanout
	ctx        context.Context
	cancel     context.CancelFunc
	cfg        can.Config
	lifecycle  lifecycle
	canType    can.Mode
	CANChannel byte
	legacyCAN  bool
	api        toomossAPI
	handle     int
	ownsDevice bool
}

func newToomoss(cfg can.Config) *Toomoss {
	return &Toomoss{
		cfg:        cfg,
		canType:    cfg.Mode,
		CANChannel: cfg.Channel,
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

func decodeToomossCANFDID(rawID uint32) (id uint32, remote bool, extended bool) {
	id = rawID & toomossCANFDIDMaskExtended
	remote = rawID&toomossCANFDIDFlagRemote != 0
	extended = rawID&toomossCANFDIDFlagExtended != 0
	return
}

func decodeToomossCANFDFlags(flags byte) (channel byte, isFD bool, brs bool, txEcho bool) {
	channel = (flags & toomossCANFDFlagChannel) >> 5
	isFD = flags&CANFD_MSG_FLAG_FDF != 0
	brs = flags&CANFD_MSG_FLAG_BRS != 0
	txEcho = flags&toomossCANFDFlagTx != 0
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

func (t *Toomoss) initialize() error {
	t.lifecycle.opMu.Lock()
	defer t.lifecycle.opMu.Unlock()
	if t.lifecycle.isInitialized() {
		return nil
	}
	cfg, err := normalizeConfig(t.cfg)
	if err != nil {
		return err
	}
	t.cfg = cfg
	t.canType = cfg.Mode
	t.CANChannel = cfg.Channel
	t.legacyCAN = false

	if !acquireToomossSession() {
		return errors.New("another Toomoss driver instance is already using the device")
	}
	t.ownsDevice = true
	opened := false
	cleanup := func(err error) error {
		if opened {
			_ = t.closeUSB()
		} else {
			t.api.release()
		}
		if t.ownsDevice {
			releaseToomossSession()
			t.ownsDevice = false
		}
		return err
	}

	api, err := loadToomossAPI()
	if err != nil {
		return cleanup(fmt.Errorf("failed to load Toomoss DLLs: %w", err))
	}
	t.api = api
	if ok, err := t.scan(); err != nil {
		return cleanup(fmt.Errorf("USB scan failed: %w", err))
	} else if !ok {
		return cleanup(errors.New("USB scan failed: device not found"))
	}
	if ok, err := t.openUSB(); err != nil {
		return cleanup(fmt.Errorf("USB open failed: %w", err))
	} else if !ok {
		return cleanup(errors.New("USB open failed"))
	}
	opened = true

	if t.canType == can.Classic {
		t.legacyCAN = true
		logf(t.cfg, "Toomoss classic CAN mode")
		if err := t.initLegacyCANDevice(); err != nil {
			return cleanup(err)
		}
		t.prepareRuntime()
		t.lifecycle.markInitialized()
		return nil
	}
	if t.api.fdSpeed == 0 || t.api.fdInit == 0 || t.api.fdStart == 0 || t.api.fdGet == 0 || t.api.fdSend == 0 {
		return cleanup(errors.New("CAN-FD APIs are not available in USB2XXX.dll"))
	}

	canFDInitConfig := defaultCANFDInitConfig()
	var fdSpeed uintptr
	if t.cfg.NominalTiming.IsZero() {
		speed, _, callErr := syscall.SyscallN(
			t.api.fdSpeed,
			uintptr(t.handle),
			uintptr(unsafe.Pointer(&canFDInitConfig)),
			uintptr(t.cfg.NominalBitrate),
			uintptr(t.cfg.DataBitrate),
		)
		if callErr != 0 {
			return cleanup(fmt.Errorf("CANFD_GetCANSpeedArg syscall failed: %w", callErr))
		}
		fdSpeed = speed
	} else {
		canFDInitConfig = applyToomossTiming(t.cfg)
	}
	canfdInit, _, callErr := syscall.SyscallN(
		t.api.fdInit,
		uintptr(t.handle),
		uintptr(t.CANChannel),
		uintptr(unsafe.Pointer(&canFDInitConfig)),
	)
	if callErr != 0 {
		return cleanup(fmt.Errorf("CANFD_Init syscall failed: %w", callErr))
	}
	fdStart, _, callErr := syscall.SyscallN(
		t.api.fdStart,
		uintptr(t.handle),
		uintptr(t.CANChannel),
	)
	if callErr != 0 {
		return cleanup(fmt.Errorf("CANFD_StartGetMsg syscall failed: %w", callErr))
	}
	time.Sleep(initDelay)
	if !(canfdInit == 0 && fdStart == 0 && fdSpeed == 0) {
		return cleanup(fmt.Errorf("CAN-FD initialization failed: CANFD_Init=%d, CANFD_StartGetMsg=%d, t.api.fdSpeed=%d", canfdInit, fdStart, fdSpeed))
	}
	t.prepareRuntime()
	t.lifecycle.markInitialized()
	logf(t.cfg, "Toomoss CAN FD initialized")
	return nil
}

func (t *Toomoss) prepareRuntime() {
	t.ctx, t.cancel = context.WithCancel(context.Background())
	t.rxChan = make(chan can.Frame, t.cfg.RxBufferSize)
	t.fanout = newRxFanout(t.ctx, t.rxChan, t.prepare(t.cfg))
}

func (t *Toomoss) initLegacyCANDevice() error {
	if t.api.canSpeed == 0 || t.api.canInit == 0 || t.api.canStart == 0 {
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
		t.api.canSpeed,
		uintptr(t.handle),
		uintptr(unsafe.Pointer(&canInitConfig)),
		uintptr(t.cfg.NominalBitrate),
	)
	if callErr != 0 {
		return fmt.Errorf("CAN_GetCANSpeedArg syscall failed: %w", callErr)
	}
	if ret != CAN_SUCCESS {
		return fmt.Errorf("CAN_GetCANSpeedArg returned %d", ret)
	}

	canInitRet, _, callErr := syscall.SyscallN(
		t.api.canInit,
		uintptr(t.handle),
		uintptr(t.CANChannel),
		uintptr(unsafe.Pointer(&canInitConfig)),
	)
	if callErr != 0 {
		return fmt.Errorf("CAN_Init syscall failed: %w", callErr)
	}
	startRet, _, callErr := syscall.SyscallN(
		t.api.canStart,
		uintptr(t.handle),
		uintptr(t.CANChannel),
	)
	if callErr != 0 {
		return fmt.Errorf("CAN_StartGetMsg syscall failed: %w", callErr)
	}
	time.Sleep(initDelay)
	if canInitRet != CAN_SUCCESS || startRet != CAN_SUCCESS {
		return fmt.Errorf("standard CAN initialization failed: CAN_Init=%d, CAN_StartGetMsg=%d", canInitRet, startRet)
	}
	logf(t.cfg, "Toomoss classic CAN initialized")
	return nil
}

func (t *Toomoss) start() error {
	t.lifecycle.opMu.Lock()
	defer t.lifecycle.opMu.Unlock()
	if !t.lifecycle.isInitialized() {
		return can.ErrNotOpen
	}
	t.drainInitialBuffer()
	if t.lifecycle.start(t.readLoop) {
		logf(t.cfg, "Toomoss backend started")
	}
	return nil
}

func (t *Toomoss) stop() {
	t.lifecycle.opMu.Lock()
	defer t.lifecycle.opMu.Unlock()
	logf(t.cfg, "Stopping Toomoss backend")
	wasInitialized := t.lifecycle.cancelAndWait(t.cancel)
	if t.fanout != nil {
		t.fanout.Close()
		t.fanout = nil
	}
	if wasInitialized {
		if err := t.closeUSB(); err != nil {
			logf(t.cfg, "Toomoss USB close failed: %v", err)
		}
	}
	if t.rxChan != nil {
		close(t.rxChan)
		t.rxChan = nil
	}
	if t.ownsDevice {
		releaseToomossSession()
		t.ownsDevice = false
	}
}

func (t *Toomoss) readLoop() {
	ticker := time.NewTicker(t.cfg.PollingInterval)
	defer ticker.Stop()
	var canMsg [msgBufferSize]CAN_MSG
	var canFDMsg [msgBufferSize]CANFD_MSG
	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			if t.legacyCAN {
				t.readClassicBurst(&canMsg)
				continue
			}
			getCanFDMsgNumRaw, _, _ := syscall.SyscallN(
				t.api.fdGet,
				uintptr(t.handle),
				uintptr(t.CANChannel),
				uintptr(unsafe.Pointer(&canFDMsg[0])),
				uintptr(len(canFDMsg)),
			)
			getCanFDMsgNum := int(int32(getCanFDMsgNumRaw))
			if getCanFDMsgNum <= 0 {
				continue
			}
			if getCanFDMsgNum > len(canFDMsg) {
				logf(t.cfg, "Toomoss returned invalid CAN-FD receive count %d", getCanFDMsgNum)
				getCanFDMsgNum = len(canFDMsg)
			}

			for i := 0; i < getCanFDMsgNum; i++ {
				msg := canFDMsg[i]
				id, remote, extended := decodeToomossCANFDID(msg.ID)
				if remote || extended || id > toomossCANFDIDMaskStandard {
					continue
				}
				_, isFD, brs, txEcho := decodeToomossCANFDFlags(msg.Flags)
				actualLen := toomossDLCToDataLen(msg.DLC, isFD)
				normalizedDLC := can.LengthToDLC(actualLen)
				direction := can.Receive
				directionLabel := "RX"
				if txEcho {
					direction = can.Transmit
					directionLabel = "TX"
				}
				unifiedMsg := can.Frame{
					Direction: direction, ID: id, DLC: normalizedDLC, Data: msg.Data, FD: isFD,
					BRS:         isFD && brs,
					TimestampUS: toomossTimestampUS(msg.TimeStampHigh, msg.TimeStamp, 10),
				}

				msgType := can.Classic
				if isFD {
					msgType = can.FD
				}
				elapsedUS, deltaUS := t.relativeLogTimes(unifiedMsg.TimestampUS, toomossTimestampWrapUS(10))
				logCANMessageRelative(t.cfg, directionLabel, unifiedMsg.ID, unifiedMsg.DLC, unifiedMsg.Data[:actualLen], msgType, elapsedUS, deltaUS)
				if txEcho && !t.cfg.IncludeTxEcho {
					t.observeBusFrameWithWrap(unifiedMsg, toomossTimestampWrapUS(10))
					continue
				}

				t.publishRxWithWrap(t.ctx, t.rxChan, unifiedMsg, toomossTimestampWrapUS(10))
			}
		}
	}
}

func (t *Toomoss) readClassicBurst(canMsg *[msgBufferSize]CAN_MSG) {
	if t.api.canGet == 0 {
		logf(t.cfg, "CAN_GetMsg not loaded")
		return
	}

	getCANMsgNumRaw, _, _ := syscall.SyscallN(
		t.api.canGet,
		uintptr(t.handle),
		uintptr(t.CANChannel),
		uintptr(unsafe.Pointer(&canMsg[0])),
	)
	getCANMsgNum := int(int32(getCANMsgNumRaw))
	if getCANMsgNum <= 0 {
		return
	}
	if getCANMsgNum > len(canMsg) {
		logf(t.cfg, "Toomoss returned invalid classic CAN receive count %d", getCANMsgNum)
		getCANMsgNum = len(canMsg)
	}

	for i := 0; i < getCANMsgNum; i++ {
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
		direction := can.Receive
		directionLabel := "RX"
		if txEcho {
			direction = can.Transmit
			directionLabel = "TX"
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
		unifiedMsg := can.Frame{
			Direction:   direction,
			ID:          id,
			DLC:         can.LengthToDLC(actualLen),
			Data:        data,
			FD:          false,
			TimestampUS: toomossTimestampUS(msg.TimeStampHigh, msg.TimeStamp, 100),
		}

		elapsedUS, deltaUS := t.relativeLogTimes(unifiedMsg.TimestampUS, toomossTimestampWrapUS(100))
		logCANMessageRelative(t.cfg, directionLabel, unifiedMsg.ID, unifiedMsg.DLC, unifiedMsg.Data[:actualLen], can.Classic, elapsedUS, deltaUS)
		if txEcho && !t.cfg.IncludeTxEcho {
			t.observeBusFrameWithWrap(unifiedMsg, toomossTimestampWrapUS(100))
			continue
		}
		t.publishRxWithWrap(t.ctx, t.rxChan, unifiedMsg, toomossTimestampWrapUS(100))
	}
}

func (t *Toomoss) drainInitialBuffer() {
	if t.legacyCAN {
		var canMsg [msgBufferSize]CAN_MSG
		for batch := 0; batch < 16; batch++ {
			nRaw, _, _ := syscall.SyscallN(
				t.api.canGet,
				uintptr(t.handle),
				uintptr(t.CANChannel),
				uintptr(unsafe.Pointer(&canMsg[0])),
			)
			if int(int32(nRaw)) <= 0 {
				return
			}
		}
		logf(t.cfg, "Toomoss initial classic CAN queue still contains frames after 16 batches")
		return
	}

	var canFDMsg [msgBufferSize]CANFD_MSG
	for batch := 0; batch < 16; batch++ {
		nRaw, _, _ := syscall.SyscallN(
			t.api.fdGet,
			uintptr(t.handle),
			uintptr(t.CANChannel),
			uintptr(unsafe.Pointer(&canFDMsg[0])),
			uintptr(len(canFDMsg)),
		)
		if int(int32(nRaw)) <= 0 {
			return
		}
	}
	logf(t.cfg, "Toomoss initial CAN-FD queue still contains frames after 16 batches")
}

func (t *Toomoss) write(frame can.Frame) error {
	id := int32(frame.ID)
	fd := frame.FD
	data := frame.Data[:frame.DataLength()]
	t.lifecycle.opMu.Lock()
	defer t.lifecycle.opMu.Unlock()
	if !t.lifecycle.isInitialized() {
		return can.ErrNotOpen
	}
	if t.legacyCAN {
		return t.writeClassicCAN(frame)
	}

	var canFDMsg [1]CANFD_MSG
	var tempData [64]byte
	copy(tempData[:], data)
	canFDMsg[0].ID = uint32(id)
	canFDMsg[0].Flags = 0
	if fd {
		canFDMsg[0].Flags = CANFD_MSG_FLAG_FDF
		if t.cfg.BRS {
			canFDMsg[0].Flags |= CANFD_MSG_FLAG_BRS
		}
	}

	transmitLen := len(data)
	if fd {
		// USB2XXX expects an actual CAN-FD payload length, not the encoded
		// 4-bit DLC. Pad unsupported intermediate lengths to the next valid size.
		transmitLen = dlcLen(can.LengthToDLC(transmitLen))
	}
	canFDMsg[0].DLC = byte(transmitLen)
	canFDMsg[0].Data = tempData

	sendRet, _, _ := syscall.SyscallN(
		t.api.fdSend,
		uintptr(t.handle),
		uintptr(t.CANChannel),
		uintptr(unsafe.Pointer(&canFDMsg[0])),
		uintptr(len(canFDMsg)),
	)

	if int(sendRet) == len(canFDMsg) {
		t.recordBusTx(frame)
	} else {
		logf(t.cfg, "CAN/CANFD send failed, ID=0x%03X", id)
		return errors.New("CAN/CANFD send failed")
	}
	return nil
}

func (t *Toomoss) writeClassicCAN(frame can.Frame) error {
	id := int32(frame.ID)
	fd := frame.FD
	data := frame.Data[:frame.DataLength()]
	if t.api.canSend == 0 {
		return errors.New("CAN_SendMsgWithTime not loaded")
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
	remoteFlag, externFlag := encodeToomossClassicFlags(t.CANChannel, false, false)
	canMsg.RemoteFlag = remoteFlag
	canMsg.ExternFlag = externFlag
	canMsg.DataLen = byte(len(data))

	sendRet, _, _ := syscall.SyscallN(
		t.api.canSend,
		uintptr(t.handle),
		uintptr(t.CANChannel),
		uintptr(unsafe.Pointer(&canMsg)),
		uintptr(1),
	)
	if int(sendRet) != 1 {
		logf(t.cfg, "error: CAN message send failed, ID=0x%03X", canID)
		return errors.New("CAN message send failed")
	}

	t.recordBusTx(frame)
	return nil
}

func (t *Toomoss) subscribe(buffer int) (<-chan can.Frame, func()) {
	t.lifecycle.opMu.Lock()
	defer t.lifecycle.opMu.Unlock()
	if t.fanout == nil {
		return nil, func() {}
	}
	return t.fanout.Subscribe(buffer)
}

func (t *Toomoss) isFD() bool {
	t.lifecycle.opMu.Lock()
	defer t.lifecycle.opMu.Unlock()
	return t.canType == can.FD
}

func (t *Toomoss) config() can.Config {
	t.lifecycle.opMu.Lock()
	defer t.lifecycle.opMu.Unlock()
	return t.cfg
}

func (t *Toomoss) setBRS(enabled bool) {
	t.lifecycle.opMu.Lock()
	defer t.lifecycle.opMu.Unlock()
	t.cfg.BRS = enabled
}

func (t *Toomoss) brs() bool {
	t.lifecycle.opMu.Lock()
	defer t.lifecycle.opMu.Unlock()
	return t.cfg.BRS
}
