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
	rxChan     chan UnifiedCANMessage
	fanout     *rxFanout
	ctx        context.Context
	cancel     context.CancelFunc
	canType    CanType
	handle     uint16
	CANChannel byte

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
	ctx, cancel := context.WithCancel(context.Background())
	handle, _ := pcanUSBHandle(int(canChannel))
	return &PCAN{
		rxChan:     make(chan UnifiedCANMessage, RxChannelBufferSize),
		fanout:     nil,
		ctx:        ctx,
		cancel:     cancel,
		canType:    canType,
		handle:     handle,
		CANChannel: canChannel,
	}
}

func (p *PCAN) Init() error {
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.rxChan = make(chan UnifiedCANMessage, RxChannelBufferSize)
	p.fanout = newRxFanout(p.ctx, p.rxChan)

	handle, err := pcanUSBHandle(int(p.CANChannel))
	if err != nil {
		return err
	}
	p.handle = handle

	if err := p.loadDLL(); err != nil {
		return err
	}

	switch p.canType {
	case CANFD:
		return p.initFD()
	case CAN:
		return p.initCAN()
	default:
		return p.initFD()
	}
}

func (p *PCAN) Start() {
	log.Println("PCAN driver started...")
	p.drainInitialBuffer()
	go p.readLoop()
}

func (p *PCAN) Stop() {
	log.Println("Stopping PCAN driver...")
	if p.cancel != nil {
		p.cancel()
	}
	if p.fanout != nil {
		p.fanout.Close()
	}
	if p.uninitProc != nil {
		_, _, _ = p.uninitProc.Call(uintptr(p.handle))
	}
}

func (p *PCAN) Write(id int32, data []byte) error {
	if len(data) == 0 {
		return errors.New("data length is 0")
	}
	if p.canType == CAN && len(data) > 8 {
		return fmt.Errorf("data length %d exceeds CAN maximum of 8", len(data))
	}
	if p.canType == CANFD && len(data) > 64 {
		return fmt.Errorf("data length %d exceeds CAN-FD maximum of 64", len(data))
	}

	switch p.canType {
	case CANFD:
		var msg pcanMsgFD
		msg.ID = uint32(id)
		msg.MsgType = pcanMessageFD | pcanMessageBRS
		msg.DLC = dataLenToDlc(len(data))
		copy(msg.Data[:], data)
		status := p.callWriteFD(&msg)
		if status != pcanErrorOK {
			return fmt.Errorf("pcan write fd failed: %s", p.formatStatus(status))
		}
		logCANMessage("TX", msg.ID, msg.DLC, msg.Data[:dlcToLen(msg.DLC)], CANFD)
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
		return nil
	default:
		return errors.New("unknown CAN type")
	}
}

func (p *PCAN) RxChan() <-chan UnifiedCANMessage {
	if p.fanout == nil {
		return nil
	}
	return p.fanout.Subscribe(RxChannelBufferSize)
}

func (p *PCAN) Context() context.Context { return p.ctx }

func (p *PCAN) initCAN() error {
	if p.initProc == nil {
		return errors.New("pcan init procedure not loaded")
	}
	status, _, _ := p.initProc.Call(
		uintptr(p.handle),
		uintptr(pcanBaud500K),
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
	bitrate, err := syscall.BytePtrFromString(pcanFDDefaultBitrate)
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
		if _, exists := seen[path]; exists {
			return
		}
		seen[path] = struct{}{}
		candidates = append(candidates, path)
	}

	if envPath := os.Getenv("PCAN_DLL_PATH"); envPath != "" {
		if info, err := os.Stat(envPath); err == nil && info.IsDir() {
			add(filepath.Join(envPath, pcanDLLName))
		} else {
			add(envPath)
		}
	}
	if envDir := os.Getenv("PCAN_DLL_DIR"); envDir != "" {
		add(filepath.Join(envDir, pcanDLLName))
	}

	add(filepath.Join(".", "DLLs", archDLLDir(), pcanDLLName))
	add(pcanDLLName)

	programRoots := []string{
		os.Getenv("ProgramFiles"),
		os.Getenv("ProgramFiles(x86)"),
	}
	archSubdirs := []string{"Win64", "x64"}
	if runtime.GOARCH == "386" {
		archSubdirs = []string{"Win32", "x86"}
	}
	baseSubdirs := []string{"", "Redistributable", "Redistributables"}

	for _, root := range programRoots {
		if root == "" {
			continue
		}
		base := filepath.Join(root, "PEAK-System", "PCAN-Basic")
		for _, sub := range baseSubdirs {
			dir := base
			if sub != "" {
				dir = filepath.Join(base, sub)
			}
			add(filepath.Join(dir, pcanDLLName))
			for _, archSub := range archSubdirs {
				add(filepath.Join(dir, archSub, pcanDLLName))
			}
		}
	}

	return candidates
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
	for {
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
}

func (p *PCAN) readLoop() {
	ticker := time.NewTicker(PollingInterval)
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
	if msgType&pcanMessageErrFrame != 0 {
		return
	}
	isFD := msgType&pcanMessageFD != 0
	msgTypeLabel := CAN
	if isFD {
		msgTypeLabel = CANFD
	}

	var unified UnifiedCANMessage
	unified.ID = id
	unified.DLC = dlc
	unified.IsFD = isFD
	copy(unified.Data[:], data)

	payloadLen := dlcToLen(dlc)
	logCANMessage("RX", unified.ID, unified.DLC, unified.Data[:payloadLen], msgTypeLabel)

	select {
	case p.rxChan <- unified:
	default:
		log.Println("Warning: receive channel full, dropping message")
	}
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
