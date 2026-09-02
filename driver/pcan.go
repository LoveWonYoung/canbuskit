//go:build windows

package driver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

const (
	pcanDLLName        = "PCANBasic.dll"
	pcanDefaultChannel = 0
	pcanUSBBaseLow     = 0x51
	pcanUSBBaseHigh    = 0x500
	pcanBaud500K       = 0x001C
	pcanTypeISA        = 0x01
	pcanIOPort         = 0x02A0
	pcanInterrupt      = 11
	pcanLangEnglish    = 0x09
	// 500k nominal / 2M data with 80 MHz clock.
	pcanFDDefaultBitrate = "f_clock_mhz=80,nom_brp=20,nom_tseg1=5,nom_tseg2=2,nom_sjw=1,data_brp=4,data_tseg1=7,data_tseg2=2,data_sjw=1"
)

const (
	pcanErrorOK        = 0x00000
	pcanErrorQRCVEmpty = 0x00020
	pcanErrorBusLight  = 0x00004
	pcanErrorBusHeavy  = 0x00008
	pcanErrorIllData   = 0x20000
)

const (
	pcanMessageStandard = 0x00
	pcanMessageRTR      = 0x01
	pcanMessageExtended = 0x02
	pcanMessageFD       = 0x04
	pcanMessageBRS      = 0x08
	pcanMessageESI      = 0x10
	pcanMessageEcho     = 0x20
	pcanMessageErrFrame = 0x40
)

type pcanMsg struct {
	ID      uint32
	MsgType uint8
	Len     uint8
	Data    [8]byte
}

type pcanMsgFD struct {
	ID      uint32
	MsgType uint8
	DLC     uint8
	Data    [64]byte
}

type pcanTimestamp struct {
	Millis         uint32
	MillisOverflow uint16
	Micros         uint16
}

type PCAN struct {
	driverObservability
	rxChan     chan CanFrame
	fanout     *rxFanout
	ctx        context.Context
	cancel     context.CancelFunc
	cfg        Config
	lifecycle  driverLifecycle
	canType    CanType
	handle     uint16
	CANChannel byte

	canFDTiming    CANFDInitConfig
	hasCANFDTiming bool

	dll              *syscall.LazyDLL
	initProc         *syscall.LazyProc
	initFDProc       *syscall.LazyProc
	uninitProc       *syscall.LazyProc
	readProc         *syscall.LazyProc
	readFDProc       *syscall.LazyProc
	writeProc        *syscall.LazyProc
	writeFDProc      *syscall.LazyProc
	getErrorTextProc *syscall.LazyProc
}

func NewPCAN(canType CanType, canChannel byte) *PCAN {
	return NewPCANWithConfig(DefaultConfig(canType, canChannel))
}

func NewPCANWithConfig(cfg Config) *PCAN {
	ctx, cancel := context.WithCancel(context.Background())
	handle, _ := pcanUSBHandle(int(cfg.Channel))
	return &PCAN{
		fanout:     nil,
		ctx:        ctx,
		cancel:     cancel,
		cfg:        cfg,
		canType:    cfg.Mode,
		handle:     handle,
		CANChannel: cfg.Channel,
	}
}

// SetCANFDInitConfig sets explicit CAN-FD bit timing used by CAN_InitializeFD.
// When set, NominalBitrate/DataBitrate are not used to derive the FD bitrate string.
func (p *PCAN) SetCANFDInitConfig(cfg CANFDInitConfig) {
	p.canFDTiming = cfg
	p.hasCANFDTiming = true
}

func (p *PCAN) SetBRS(enabled bool) {
	p.lifecycle.opMu.Lock()
	defer p.lifecycle.opMu.Unlock()
	p.cfg.BRS = enabled
}

func (p *PCAN) BRS() bool {
	p.lifecycle.opMu.Lock()
	defer p.lifecycle.opMu.Unlock()
	return p.cfg.BRS
}

func (p *PCAN) Init() error {
	p.lifecycle.opMu.Lock()
	defer p.lifecycle.opMu.Unlock()
	if p.lifecycle.isInitialized() {
		return nil
	}

	cfg, err := normalizeConfig(p.cfg)
	if err != nil {
		return err
	}
	p.cfg = cfg
	p.canType = cfg.Mode
	p.CANChannel = cfg.Channel
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.rxChan = make(chan CanFrame, cfg.RxBufferSize)
	p.fanout = newRxFanout(p.ctx, p.rxChan, p.resetTelemetryWith(cfg))
	cleanup := func(err error) error {
		p.cancel()
		p.fanout.Close()
		close(p.rxChan)
		p.fanout = nil
		p.rxChan = nil
		p.closeTelemetry()
		return err
	}

	handle, err := pcanUSBHandle(int(p.CANChannel))
	if err != nil {
		return cleanup(err)
	}
	p.handle = handle

	if err := p.loadDLL(); err != nil {
		return cleanup(err)
	}

	var initErr error
	switch p.canType {
	case CANFD:
		initErr = p.initFD()
	case CAN:
		initErr = p.initCAN()
	default:
		initErr = fmt.Errorf("unknown CAN type: %d", p.canType)
	}
	if initErr != nil {
		return cleanup(initErr)
	}
	p.lifecycle.markInitialized()
	return nil
}

func (p *PCAN) Start() {
	if err := p.StartWithError(); err != nil {
		log.Printf("PCAN start failed: %v", err)
	}
}

func (p *PCAN) StartWithError() error {
	p.lifecycle.opMu.Lock()
	defer p.lifecycle.opMu.Unlock()
	if !p.lifecycle.isInitialized() {
		return fmt.Errorf("%w: PCAN", ErrDriverNotInitialized)
	}
	p.drainInitialBuffer()
	if p.lifecycle.start(p.readLoop) {
		log.Println("PCAN can_driver started...")
	}
	return nil
}

func (p *PCAN) Stop() {
	p.lifecycle.opMu.Lock()
	defer p.lifecycle.opMu.Unlock()
	log.Println("Stopping PCAN driver...")
	wasInitialized := p.lifecycle.cancelAndWait(p.cancel)
	if p.fanout != nil {
		p.fanout.Close()
		p.fanout = nil
	}
	if wasInitialized && p.uninitProc != nil {
		_, _, _ = p.uninitProc.Call(uintptr(p.handle))
	}
	if p.rxChan != nil {
		close(p.rxChan)
		p.rxChan = nil
	}
	p.closeTelemetry()
}

func (p *PCAN) Write(id int32, fd bool, data []byte) error {
	p.lifecycle.opMu.Lock()
	defer p.lifecycle.opMu.Unlock()
	if !p.lifecycle.isInitialized() {
		return errors.New("PCAN driver is not initialized")
	}
	if err := validateWrite(p.cfg, id, fd, data); err != nil {
		return err
	}

	if fd {
		if p.canType != CANFD {
			return errors.New("PCAN is not initialized in CAN-FD mode")
		}
		var msg pcanMsgFD
		msg.ID = uint32(id)
		msg.MsgType = pcanMessageFD
		if p.cfg.BRS {
			msg.MsgType |= pcanMessageBRS
		}
		msg.DLC = dataLenToDlc(len(data))
		copy(msg.Data[:], data)
		status := p.callWriteFD(&msg)
		if status != pcanErrorOK {
			return fmt.Errorf("pcan write fd failed: %s", p.formatStatus(status))
		}
		logCANMessage("TX", msg.ID, msg.DLC, msg.Data[:dlcToLen(msg.DLC)], CANFD)
		p.recordBusTx(id, true, p.cfg.BRS, data)
		return nil
	}

	switch p.canType {
	case CANFD:
		var msg pcanMsgFD
		msg.ID = uint32(id)
		msg.MsgType = pcanMessageStandard
		msg.DLC = dataLenToDlc(len(data))
		copy(msg.Data[:], data)
		status := p.callWriteFD(&msg)
		if status != pcanErrorOK {
			return fmt.Errorf("pcan write failed: %s", p.formatStatus(status))
		}
		logCANMessage("TX", msg.ID, msg.DLC, msg.Data[:len(data)], CAN)
		p.recordBusTx(id, false, false, data)
		return nil
	case CAN:
		var msg pcanMsg
		msg.ID = uint32(id)
		msg.MsgType = pcanMessageStandard
		msg.Len = byte(len(data))
		copy(msg.Data[:], data)
		status := p.callWrite(&msg)
		if status != pcanErrorOK {
			return fmt.Errorf("pcan write failed: %s", p.formatStatus(status))
		}
		logCANMessage("TX", msg.ID, msg.Len, msg.Data[:msg.Len], CAN)
		p.recordBusTx(id, false, false, data)
		return nil
	default:
		return errors.New("unknown CAN type")
	}
}

func (p *PCAN) RxChan() <-chan CanFrame {
	p.lifecycle.opMu.Lock()
	defer p.lifecycle.opMu.Unlock()
	if p.fanout == nil {
		return nil
	}
	ch, _ := p.fanout.Subscribe(p.cfg.RxBufferSize)
	return ch
}

func (p *PCAN) SubscribeRx(buffer int) (<-chan CanFrame, func()) {
	p.lifecycle.opMu.Lock()
	defer p.lifecycle.opMu.Unlock()
	if p.fanout == nil {
		return nil, func() {}
	}
	return p.fanout.Subscribe(buffer)
}

func (p *PCAN) Config() Config {
	p.lifecycle.opMu.Lock()
	defer p.lifecycle.opMu.Unlock()
	return p.cfg
}

func (p *PCAN) initCAN() error {
	if p.initProc == nil {
		return errors.New("pcan init procedure not loaded")
	}
	baud := pcanClassicBaud(p.cfg.NominalBitrate)
	if baud == 0 {
		return fmt.Errorf("unsupported PCAN classic CAN bitrate: %d", p.cfg.NominalBitrate)
	}
	status, _, _ := p.initProc.Call(
		uintptr(p.handle),
		uintptr(baud),
		uintptr(pcanTypeISA),
		uintptr(pcanIOPort),
		uintptr(pcanInterrupt),
	)
	if uint32(status) != pcanErrorOK {
		return fmt.Errorf("pcan init failed: %s", p.formatStatus(uint32(status)))
	}
	return nil
}

func (p *PCAN) initFD() error {
	if p.initFDProc == nil {
		return errors.New("pcan init fd procedure not loaded")
	}
	var (
		bitrateConfig string
		err           error
	)
	if p.hasCANFDTiming {
		bitrateConfig = pcanFDBitrateFromTiming(p.canFDTiming)
	} else {
		bitrateConfig, err = pcanFDBitrate(p.cfg.NominalBitrate, p.cfg.DataBitrate)
		if err != nil {
			return err
		}
	}
	bitrate, err := syscall.BytePtrFromString(bitrateConfig)
	if err != nil {
		return fmt.Errorf("pcan fd bitrate string invalid: %w", err)
	}
	status, _, _ := p.initFDProc.Call(
		uintptr(p.handle),
		uintptr(unsafe.Pointer(bitrate)),
	)
	if uint32(status) != pcanErrorOK {
		return fmt.Errorf("pcan init fd failed: %s", p.formatStatus(uint32(status)))
	}
	return nil
}

func pcanDLLCandidates() []string {
	var candidates []string
	seen := make(map[string]struct{})

	add := func(path string) {
		if path == "" {
			return
		}
		normalized := strings.ToLower(filepath.Clean(path))
		if _, exists := seen[normalized]; exists {
			return
		}
		seen[normalized] = struct{}{}
		candidates = append(candidates, path)
	}

	if path, err := getPCANBasicDLLFromRegistry(); err == nil && path != "" {
		add(path)
	}

	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		systemRoot = `C:\Windows`
	}
	if runtime.GOARCH == "386" {
		add(filepath.Join(systemRoot, "SysWOW64", pcanDLLName))
	}
	add(filepath.Join(systemRoot, "System32", pcanDLLName))
	add(filepath.Join(".", "bin", pcanDLLName))
	return candidates
}

func getPCANBasicDLLFromRegistry() (string, error) {
	access := uint32(registry.QUERY_VALUE)
	if runtime.GOARCH == "386" {
		access |= registry.WOW64_32KEY
	} else {
		access |= registry.WOW64_64KEY
	}

	key, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows\CurrentVersion\SharedDlls`,
		access,
	)
	if err != nil {
		return "", err
	}
	defer key.Close()

	names, err := key.ReadValueNames(-1)
	if err != nil {
		return "", err
	}
	for _, name := range names {
		if strings.EqualFold(filepath.Base(name), pcanDLLName) {
			return name, nil
		}
	}
	return "", fmt.Errorf("%s not found in SharedDlls", pcanDLLName)
}

func (p *PCAN) loadDLL() error {
	candidates := pcanDLLCandidates()
	var errs []string
	for _, dllPath := range candidates {
		dll := syscall.NewLazyDLL(dllPath)
		if err := dll.Load(); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", dllPath, err))
			continue
		}
		p.dll = dll
		p.initProc = dll.NewProc("CAN_Initialize")
		p.initFDProc = dll.NewProc("CAN_InitializeFD")
		p.uninitProc = dll.NewProc("CAN_Uninitialize")
		p.readProc = dll.NewProc("CAN_Read")
		p.readFDProc = dll.NewProc("CAN_ReadFD")
		p.writeProc = dll.NewProc("CAN_Write")
		p.writeFDProc = dll.NewProc("CAN_WriteFD")
		p.getErrorTextProc = dll.NewProc("CAN_GetErrorText")
		return nil
	}
	return fmt.Errorf("failed to load %s (%s)", pcanDLLName, strings.Join(errs, "; "))
}

func (p *PCAN) drainInitialBuffer() {
	for i := 0; i < MsgBufferSize; i++ {
		if p.canType == CANFD {
			var msg pcanMsgFD
			var ts uint64
			status := p.callReadFD(&msg, &ts)
			if status == pcanErrorQRCVEmpty {
				return
			}
		} else {
			var msg pcanMsg
			var ts pcanTimestamp
			status := p.callRead(&msg, &ts)
			if status == pcanErrorQRCVEmpty {
				return
			}
		}
	}
	log.Printf("PCAN initial receive queue still contains frames after draining %d entries", MsgBufferSize)
}

func (p *PCAN) readLoop() {
	ticker := time.NewTicker(p.cfg.PollingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.readBurst()
		}
	}
}

func (p *PCAN) readBurst() {
	for {
		if p.canType == CANFD {
			var msg pcanMsgFD
			var ts uint64
			status := p.callReadFD(&msg, &ts)
			if !p.handleReadStatus(status) {
				return
			}
			if status != pcanErrorOK {
				continue
			}
			p.enqueueMessage(msg.ID, msg.DLC, msg.Data[:], msg.MsgType)
		} else {
			var msg pcanMsg
			var ts pcanTimestamp
			status := p.callRead(&msg, &ts)
			if !p.handleReadStatus(status) {
				return
			}
			if status != pcanErrorOK {
				continue
			}
			p.enqueueMessage(msg.ID, msg.Len, msg.Data[:], msg.MsgType)
		}
	}
}

func (p *PCAN) handleReadStatus(status uint32) bool {
	switch {
	case status == pcanErrorOK:
		return true
	case status == pcanErrorQRCVEmpty:
		return false
	case status&(pcanErrorBusLight|pcanErrorBusHeavy) != 0:
		log.Printf("PCAN bus warning: %s", p.formatStatus(status))
		return true
	case status == pcanErrorIllData:
		return true
	default:
		log.Printf("PCAN read error: %s", p.formatStatus(status))
		return false
	}
}

func (p *PCAN) enqueueMessage(id uint32, dlc byte, data []byte, msgType uint8) {
	if msgType&(pcanMessageRTR|pcanMessageErrFrame|pcanMessageExtended) != 0 || id > 0x7FF {
		return
	}
	isFD := msgType&pcanMessageFD != 0
	msgTypeLabel := CAN
	if isFD {
		msgTypeLabel = CANFD
	}

	var unified CanFrame
	unified.Direction = RX
	if msgType&pcanMessageEcho != 0 {
		unified.Direction = TX
	}
	unified.ID = id
	unified.DLC = dlc
	unified.IsFD = isFD
	unified.BRS = isFD && msgType&pcanMessageBRS != 0
	copy(unified.Data[:], data)
	if unified.Direction == TX && !p.cfg.IncludeTxEcho {
		p.observeBusFrame(unified)
		return
	}

	payloadLen := dlcToLen(dlc)
	logCANMessage("RX", unified.ID, unified.DLC, unified.Data[:payloadLen], msgTypeLabel)

	p.publishRx(p.ctx, p.rxChan, unified)
}

func (p *PCAN) callRead(msg *pcanMsg, ts *pcanTimestamp) uint32 {
	if p.readProc == nil {
		return pcanErrorIllData
	}
	status, _, _ := p.readProc.Call(
		uintptr(p.handle),
		uintptr(unsafe.Pointer(msg)),
		uintptr(unsafe.Pointer(ts)),
	)
	return uint32(status)
}

func (p *PCAN) callReadFD(msg *pcanMsgFD, ts *uint64) uint32 {
	if p.readFDProc == nil {
		return pcanErrorIllData
	}
	status, _, _ := p.readFDProc.Call(
		uintptr(p.handle),
		uintptr(unsafe.Pointer(msg)),
		uintptr(unsafe.Pointer(ts)),
	)
	return uint32(status)
}

func (p *PCAN) callWrite(msg *pcanMsg) uint32 {
	if p.writeProc == nil {
		return pcanErrorIllData
	}
	status, _, _ := p.writeProc.Call(
		uintptr(p.handle),
		uintptr(unsafe.Pointer(msg)),
	)
	return uint32(status)
}

func (p *PCAN) callWriteFD(msg *pcanMsgFD) uint32 {
	if p.writeFDProc == nil {
		return pcanErrorIllData
	}
	status, _, _ := p.writeFDProc.Call(
		uintptr(p.handle),
		uintptr(unsafe.Pointer(msg)),
	)
	return uint32(status)
}

func (p *PCAN) formatStatus(status uint32) string {
	if p.getErrorTextProc == nil {
		return fmt.Sprintf("pcan status 0x%X", status)
	}
	var buf [256]byte
	ret, _, _ := p.getErrorTextProc.Call(
		uintptr(status),
		uintptr(pcanLangEnglish),
		uintptr(unsafe.Pointer(&buf[0])),
	)
	if uint32(ret) != pcanErrorOK {
		return fmt.Sprintf("pcan status 0x%X", status)
	}
	if n := bytes.IndexByte(buf[:], 0); n >= 0 {
		return string(buf[:n])
	}
	return string(buf[:])
}

func pcanUSBHandle(channel int) (uint16, error) {
	if channel < 0 || channel > 15 {
		return 0, fmt.Errorf("pcan channel %d out of range (0-15)", channel)
	}
	if channel < 8 {
		return uint16(pcanUSBBaseLow + channel), nil
	}
	return uint16(pcanUSBBaseHigh + channel + 1), nil
}

func (p *PCAN) IsFDMode() bool {
	p.lifecycle.opMu.Lock()
	defer p.lifecycle.opMu.Unlock()
	return p.canType == CANFD
}

func pcanClassicBaud(bitrate uint32) uint16 {
	switch bitrate {
	case 1_000_000:
		return 0x0014
	case 800_000:
		return 0x0016
	case 500_000:
		return pcanBaud500K
	case 250_000:
		return 0x011C
	case 125_000:
		return 0x031C
	case 100_000:
		return 0x432F
	case 50_000:
		return 0x472F
	case 20_000:
		return 0x532F
	case 10_000:
		return 0x672F
	case 5_000:
		return 0x7F7F
	default:
		return 0
	}
}

func pcanFDBitrate(nominal, data uint32) (string, error) {
	if nominal == 500_000 && data == 2_000_000 {
		return pcanFDDefaultBitrate, nil
	}
	nomBRP, nomTseg1, nomTseg2, err := findPCANBitTiming(nominal, 256, 128)
	if err != nil {
		return "", fmt.Errorf("unsupported PCAN nominal bitrate %d: %w", nominal, err)
	}
	dataBRP, dataTseg1, dataTseg2, err := findPCANBitTiming(data, 32, 16)
	if err != nil {
		return "", fmt.Errorf("unsupported PCAN data bitrate %d: %w", data, err)
	}
	return fmt.Sprintf(
		"f_clock_mhz=80,nom_brp=%d,nom_tseg1=%d,nom_tseg2=%d,nom_sjw=%d,data_brp=%d,data_tseg1=%d,data_tseg2=%d,data_sjw=%d",
		nomBRP, nomTseg1, nomTseg2, min(nomTseg2, 4),
		dataBRP, dataTseg1, dataTseg2, min(dataTseg2, 4),
	), nil
}

func pcanFDBitrateFromTiming(cfg CANFDInitConfig) string {
	return fmt.Sprintf(
		"f_clock_mhz=80,nom_brp=%d,nom_tseg1=%d,nom_tseg2=%d,nom_sjw=%d,data_brp=%d,data_tseg1=%d,data_tseg2=%d,data_sjw=%d",
		cfg.NBT_BRP, cfg.NBT_SEG1, cfg.NBT_SEG2, cfg.NBT_SJW,
		cfg.DBT_BRP, cfg.DBT_SEG1, cfg.DBT_SEG2, cfg.DBT_SJW,
	)
}

func findPCANBitTiming(bitrate uint32, maxTseg1, maxTseg2 int) (brp, tseg1, tseg2 int, err error) {
	if bitrate == 0 || 80_000_000%bitrate != 0 {
		return 0, 0, 0, errors.New("bitrate cannot be represented with an 80 MHz clock")
	}
	targetTQ := int(80_000_000 / bitrate)
	bestError := 101
	for candidateBRP := 1; candidateBRP <= 1024; candidateBRP++ {
		if targetTQ%candidateBRP != 0 {
			continue
		}
		totalTQ := targetTQ / candidateBRP
		for candidateTseg2 := 1; candidateTseg2 <= maxTseg2; candidateTseg2++ {
			candidateTseg1 := totalTQ - 1 - candidateTseg2
			if candidateTseg1 < 1 || candidateTseg1 > maxTseg1 {
				continue
			}
			samplePoint := (1 + candidateTseg1) * 100 / totalTQ
			sampleError := samplePoint - 80
			if sampleError < 0 {
				sampleError = -sampleError
			}
			if sampleError < bestError {
				brp, tseg1, tseg2 = candidateBRP, candidateTseg1, candidateTseg2
				bestError = sampleError
			}
		}
	}
	if brp == 0 {
		return 0, 0, 0, errors.New("no valid bit timing found")
	}
	return brp, tseg1, tseg2, nil
}
