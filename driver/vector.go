//go:build windows

package driver

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	vectorDLLName32 = "vxlapi.dll"
	vectorDLLName64 = "vxlapi64.dll"

	//	vectorDefaultHwTypeVN1640 = 59
	vectorDefaultHwIndex = 0
	//	vectorDefaultChannel      = 2 // Vector API channel is 0-based; channel 1 in UI maps to 0 here.

	vectorDefaultBitrate     = 500000
	vectorDefaultDataBitrate = 2000000

	vectorDefaultCanFdSJW   = 2
	vectorDefaultCanFdTseg1 = 6
	vectorDefaultCanFdTseg2 = 3

	vectorDefaultRxQueueSize = 16384

	vectorBusTypeCAN           = 1
	vectorInterfaceVersion     = 3
	vectorInterfaceVersionV4   = 4
	vectorOutputModeNormal     = 1
	vectorInvalidPortHandle    = -1
	vectorStatusSuccess        = 0
	vectorStatusQueueIsEmpty   = 10
	vectorEventTagReceiveMsg   = 1
	vectorEventTagTransmitMsg  = 10
	vectorCanMsgFlagErrorFrame = 1

	vectorCanFdTagRxOK = 1024
	vectorCanFdTagTxOK = 1028

	vectorCanFdRxFlagEDL = 1

	vectorCanFdTxTag     = 1088
	vectorCanFdTxFlagEDL = 1
	vectorCanFdTxFlagBRS = 2
)

type xlCanMsg struct {
	ID    uint32
	Flags uint16
	DLC   uint16
	Res1  uint64
	Data  [8]byte
	Res2  uint64
}

type xlEventTagData struct {
	Msg xlCanMsg
}

type xlEvent struct {
	Tag        uint8
	ChanIndex  uint8
	TransID    uint16
	PortHandle uint16
	Flags      uint8
	Reserved   uint8
	TimeStamp  uint64
	TagData    xlEventTagData
}

type xlCanRxMsg struct {
	CanID       uint32
	MsgFlags    uint32
	CRC         uint32
	Reserved1   [12]byte
	TotalBitCnt uint16
	DLC         uint8
	Reserved    [5]byte
	Data        [64]byte
}

type xlCanTxMsg struct {
	CanID    uint32
	MsgFlags uint32
	DLC      uint8
	Reserved [7]byte
	Data     [64]byte
}

type xlCanRxTagData struct {
	CanRxOkMsg xlCanRxMsg
}

type xlCanTxTagData struct {
	CanMsg xlCanTxMsg
}

type xlCanRxEvent struct {
	Size       int32
	Tag        uint16
	ChanIndex  uint8
	Reserved   uint8
	UserHandle int32
	FlagsChip  uint16
	Reserved0  uint16
	Reserved1  uint64
	TimeStamp  uint64
	TagData    xlCanRxTagData
}

type xlCanTxEvent struct {
	Tag       uint16
	TransID   uint16
	ChanIndex uint8
	Reserved  [3]byte
	TagData   xlCanTxTagData
}

type xlCanFdConf struct {
	ArbitrationBitRate uint32
	SjwAbr             uint32
	Tseg1Abr           uint32
	Tseg2Abr           uint32
	DataBitRate        uint32
	SjwDbr             uint32
	Tseg1Dbr           uint32
	Tseg2Dbr           uint32
	Reserved           uint8
	Options            uint8
	Reserved1          [2]byte
	Reserved2          uint8
}

type Vector struct {
	canType CanType
	rxChan  chan UnifiedCANMessage
	fanout  *rxFanout
	ctx     context.Context
	cancel  context.CancelFunc

	dll *syscall.LazyDLL

	openDriverProc            *syscall.LazyProc
	closeDriverProc           *syscall.LazyProc
	openPortProc              *syscall.LazyProc
	closePortProc             *syscall.LazyProc
	getChannelIndexProc       *syscall.LazyProc
	activateChannelProc       *syscall.LazyProc
	deactivateChannelProc     *syscall.LazyProc
	canSetChannelBitrateProc  *syscall.LazyProc
	canFdSetConfigurationProc *syscall.LazyProc
	canSetChannelOutputProc   *syscall.LazyProc
	canSetChannelModeProc     *syscall.LazyProc
	receiveProc               *syscall.LazyProc
	canReceiveProc            *syscall.LazyProc
	canTransmitProc           *syscall.LazyProc
	canTransmitExProc         *syscall.LazyProc
	getErrorStringProc        *syscall.LazyProc

	portHandle     int32
	channelIndex   int32
	channelMask    uint64
	permissionMask uint64

	DeviceType int
	CANChannel int
}

func NewVector(canType CanType, deviceType int, canChannel int) *Vector {
	ctx, cancel := context.WithCancel(context.Background())
	return &Vector{
		canType:    canType,
		rxChan:     make(chan UnifiedCANMessage, RxChannelBufferSize),
		fanout:     nil,
		ctx:        ctx,
		cancel:     cancel,
		portHandle: vectorInvalidPortHandle,
		DeviceType: deviceType,
		CANChannel: canChannel,
	}
}

func (v *Vector) Init() error {
	v.ctx, v.cancel = context.WithCancel(context.Background())
	v.rxChan = make(chan UnifiedCANMessage, RxChannelBufferSize)
	v.fanout = newRxFanout(v.ctx, v.rxChan)
	v.portHandle = vectorInvalidPortHandle

	if err := v.loadDLL(); err != nil {
		return err
	}
	if err := v.callStatus(v.openDriverProc); err != nil {
		return v.cleanup(fmt.Errorf("xlOpenDriver failed: %w", err))
	}

	if err := v.selectChannel(); err != nil {
		return v.cleanup(err)
	}

	if err := v.openPort(); err != nil {
		return v.cleanup(err)
	}

	if err := v.configureChannel(); err != nil {
		return v.cleanup(err)
	}

	log.Println("Vector driver initialized successfully.")
	return nil
}

func (v *Vector) Start() {
	if v.portHandle == vectorInvalidPortHandle {
		log.Println("Vector not initialized, cannot start")
		return
	}
	go v.readLoop()
}

func (v *Vector) Stop() {
	if v.cancel != nil {
		v.cancel()
	}

	if v.fanout != nil {
		v.fanout.Close()
	}

	if v.deactivateChannelProc != nil && v.portHandle != vectorInvalidPortHandle {
		_, _, _ = v.deactivateChannelProc.Call(uintptr(v.portHandle), uintptr(v.channelMask))
	}
	if v.closePortProc != nil && v.portHandle != vectorInvalidPortHandle {
		_, _, _ = v.closePortProc.Call(uintptr(v.portHandle))
	}
	if v.closeDriverProc != nil {
		_, _, _ = v.closeDriverProc.Call()
	}

	if v.rxChan != nil {
		close(v.rxChan)
		v.rxChan = nil
	}
	v.portHandle = vectorInvalidPortHandle
}

func (v *Vector) Write(id int32, data []byte) error {
	if len(data) == 0 {
		return errors.New("data length is 0")
	}
	if v.canType == CAN && len(data) > 8 {
		return fmt.Errorf("data length %d exceeds CAN maximum of 8", len(data))
	}
	if v.canType == CANFD && len(data) > 64 {
		return fmt.Errorf("data length %d exceeds CAN-FD maximum of 64", len(data))
	}

	switch v.canType {
	case CANFD:
		var txEvent xlCanTxEvent
		txEvent.Tag = vectorCanFdTxTag
		txEvent.TransID = 0xFFFF
		txEvent.TagData.CanMsg.CanID = uint32(id)
		// txEvent.TagData.CanMsg.MsgFlags = vectorCanFdTxFlagEDL | vectorCanFdTxFlagBRS
		txEvent.TagData.CanMsg.MsgFlags = vectorCanFdTxFlagEDL
		txEvent.TagData.CanMsg.DLC = dataLenToDlc(len(data))
		copy(txEvent.TagData.CanMsg.Data[:], data)

		msgCount := uint32(1)
		msgSent := uint32(0)
		status, _, _ := v.canTransmitExProc.Call(
			uintptr(v.portHandle),
			uintptr(v.channelMask),
			uintptr(msgCount),
			uintptr(unsafe.Pointer(&msgSent)),
			uintptr(unsafe.Pointer(&txEvent)),
		)
		if int32(status) != vectorStatusSuccess {
			return fmt.Errorf("xlCanTransmitEx failed: %s", v.errorString(int16(status)))
		}
		logCANMessage("TX", uint32(id), txEvent.TagData.CanMsg.DLC, txEvent.TagData.CanMsg.Data[:dlcToLen(txEvent.TagData.CanMsg.DLC)], CANFD)
		return nil
	case CAN:
		var event xlEvent
		event.Tag = vectorEventTagTransmitMsg
		event.TagData.Msg.ID = uint32(id)
		event.TagData.Msg.DLC = uint16(len(data))
		copy(event.TagData.Msg.Data[:], data)

		msgCount := uint32(1)
		status, _, _ := v.canTransmitProc.Call(
			uintptr(v.portHandle),
			uintptr(v.channelMask),
			uintptr(unsafe.Pointer(&msgCount)),
			uintptr(unsafe.Pointer(&event)),
		)
		if int32(status) != vectorStatusSuccess {
			return fmt.Errorf("xlCanTransmit failed: %s", v.errorString(int16(status)))
		}
		payloadLen := int(event.TagData.Msg.DLC)
		logCANMessage("TX", uint32(id), byte(event.TagData.Msg.DLC), event.TagData.Msg.Data[:payloadLen], CAN)
		return nil
	default:
		return errors.New("unknown CAN type")
	}
}

func (v *Vector) RxChan() <-chan UnifiedCANMessage {
	if v.fanout == nil {
		return nil
	}
	return v.fanout.Subscribe(RxChannelBufferSize)
}

func (v *Vector) Context() context.Context { return v.ctx }

func (v *Vector) loadDLL() error {
	if v.dll != nil {
		return nil
	}
	dllName := vectorDLLName64
	if runtime.GOARCH == "386" {
		dllName = vectorDLLName32
	}
	candidates := []string{
		filepath.Join(".", "DLLs", archDLLDir(), dllName),
		dllName,
	}
	var errs []string
	for _, dllPath := range candidates {
		dll := syscall.NewLazyDLL(dllPath)
		if err := dll.Load(); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", dllPath, err))
			continue
		}
		v.dll = dll
		v.openDriverProc = dll.NewProc("xlOpenDriver")
		v.closeDriverProc = dll.NewProc("xlCloseDriver")
		v.openPortProc = dll.NewProc("xlOpenPort")
		v.closePortProc = dll.NewProc("xlClosePort")
		v.getChannelIndexProc = dll.NewProc("xlGetChannelIndex")
		v.activateChannelProc = dll.NewProc("xlActivateChannel")
		v.deactivateChannelProc = dll.NewProc("xlDeactivateChannel")
		v.canSetChannelBitrateProc = dll.NewProc("xlCanSetChannelBitrate")
		v.canFdSetConfigurationProc = dll.NewProc("xlCanFdSetConfiguration")
		v.canSetChannelOutputProc = dll.NewProc("xlCanSetChannelOutput")
		v.canSetChannelModeProc = dll.NewProc("xlCanSetChannelMode")
		v.receiveProc = dll.NewProc("xlReceive")
		v.canReceiveProc = dll.NewProc("xlCanReceive")
		v.canTransmitProc = dll.NewProc("xlCanTransmit")
		v.canTransmitExProc = dll.NewProc("xlCanTransmitEx")
		v.getErrorStringProc = dll.NewProc("xlGetErrorString")
		break
	}

	if v.dll == nil {
		return fmt.Errorf("failed to load %s (%s)", dllName, strings.Join(errs, "; "))
	}

	for name, proc := range map[string]*syscall.LazyProc{
		"xlOpenDriver":            v.openDriverProc,
		"xlCloseDriver":           v.closeDriverProc,
		"xlOpenPort":              v.openPortProc,
		"xlClosePort":             v.closePortProc,
		"xlGetChannelIndex":       v.getChannelIndexProc,
		"xlActivateChannel":       v.activateChannelProc,
		"xlDeactivateChannel":     v.deactivateChannelProc,
		"xlCanSetChannelBitrate":  v.canSetChannelBitrateProc,
		"xlCanFdSetConfiguration": v.canFdSetConfigurationProc,
		"xlCanSetChannelOutput":   v.canSetChannelOutputProc,
		"xlCanSetChannelMode":     v.canSetChannelModeProc,
		"xlReceive":               v.receiveProc,
		"xlCanReceive":            v.canReceiveProc,
		"xlCanTransmit":           v.canTransmitProc,
		"xlCanTransmitEx":         v.canTransmitExProc,
		"xlGetErrorString":        v.getErrorStringProc,
	} {
		if err := proc.Find(); err != nil {
			return fmt.Errorf("vector proc %s not found: %w", name, err)
		}
	}

	return nil
}

func (v *Vector) selectChannel() error {
	if v.getChannelIndexProc == nil {
		return errors.New("xlGetChannelIndex not loaded")
	}
	r1, _, _ := v.getChannelIndexProc.Call(
		uintptr(v.DeviceType),
		uintptr(vectorDefaultHwIndex),
		uintptr(v.CANChannel),
	)
	idx := int32(r1)
	if idx < 0 || idx > 63 {
		return fmt.Errorf("vector channel not found (hwType=%d hwIndex=%d hwChannel=%d)", v.DeviceType, vectorDefaultHwIndex, v.CANChannel)
	}
	v.channelIndex = idx
	v.channelMask = uint64(1) << uint(idx)
	return nil
}

func (v *Vector) openPort() error {
	if v.openPortProc == nil {
		return errors.New("xlOpenPort not loaded")
	}
	appName, _ := syscall.BytePtrFromString("")
	permissionMask := v.channelMask
	interfaceVersion := vectorInterfaceVersion
	if v.canType == CANFD {
		interfaceVersion = vectorInterfaceVersionV4
	}
	status, _, _ := v.openPortProc.Call(
		uintptr(unsafe.Pointer(&v.portHandle)),
		uintptr(unsafe.Pointer(appName)),
		uintptr(v.channelMask),
		uintptr(unsafe.Pointer(&permissionMask)),
		uintptr(vectorDefaultRxQueueSize),
		uintptr(interfaceVersion),
		uintptr(vectorBusTypeCAN),
	)
	if int32(status) != vectorStatusSuccess {
		return fmt.Errorf("xlOpenPort failed: %s", v.errorString(int16(status)))
	}
	v.permissionMask = permissionMask
	if v.portHandle == vectorInvalidPortHandle {
		return errors.New("xlOpenPort returned invalid port handle")
	}
	return nil
}

func (v *Vector) configureChannel() error {
	channelMask := v.channelMask & v.permissionMask
	if channelMask == 0 {
		return errors.New("no init access for selected channel")
	}

	if v.canType == CANFD {
		var conf xlCanFdConf
		conf.ArbitrationBitRate = vectorDefaultBitrate
		conf.DataBitRate = vectorDefaultDataBitrate
		conf.SjwAbr = vectorDefaultCanFdSJW
		conf.Tseg1Abr = vectorDefaultCanFdTseg1
		conf.Tseg2Abr = vectorDefaultCanFdTseg2
		conf.SjwDbr = vectorDefaultCanFdSJW
		conf.Tseg1Dbr = vectorDefaultCanFdTseg1
		conf.Tseg2Dbr = vectorDefaultCanFdTseg2

		if err := v.callStatus(v.canFdSetConfigurationProc, uintptr(v.portHandle), uintptr(channelMask), uintptr(unsafe.Pointer(&conf))); err != nil {
			return fmt.Errorf("xlCanFdSetConfiguration failed: %w", err)
		}
	} else {
		if err := v.callStatus(v.canSetChannelBitrateProc, uintptr(v.portHandle), uintptr(channelMask), uintptr(vectorDefaultBitrate)); err != nil {
			return fmt.Errorf("xlCanSetChannelBitrate failed: %w", err)
		}
	}

	if err := v.callStatus(v.canSetChannelOutputProc, uintptr(v.portHandle), uintptr(channelMask), uintptr(vectorOutputModeNormal)); err != nil {
		return fmt.Errorf("xlCanSetChannelOutput failed: %w", err)
	}

	if err := v.callStatus(v.canSetChannelModeProc, uintptr(v.portHandle), uintptr(channelMask), uintptr(0), uintptr(0)); err != nil {
		return fmt.Errorf("xlCanSetChannelMode failed: %w", err)
	}

	if err := v.callStatus(v.activateChannelProc, uintptr(v.portHandle), uintptr(channelMask), uintptr(vectorBusTypeCAN), uintptr(0)); err != nil {
		return fmt.Errorf("xlActivateChannel failed: %w", err)
	}

	return nil
}

func (v *Vector) readLoop() {
	ticker := time.NewTicker(PollingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-v.ctx.Done():
			return
		case <-ticker.C:
			v.readBurst()
		}
	}
}

func (v *Vector) readBurst() {
	for {
		if v.canType == CANFD {
			if !v.readOneCanFD() {
				return
			}
		} else {
			if !v.readOneCAN() {
				return
			}
		}
	}
}

func (v *Vector) readOneCanFD() bool {
	var event xlCanRxEvent
	event.Size = int32(unsafe.Sizeof(event))
	status, _, _ := v.canReceiveProc.Call(
		uintptr(v.portHandle),
		uintptr(unsafe.Pointer(&event)),
	)
	switch int32(status) {
	case vectorStatusSuccess:
	case vectorStatusQueueIsEmpty:
		return false
	default:
		log.Printf("Vector CAN-FD receive error: %s", v.errorString(int16(status)))
		return false
	}

	if event.Tag != vectorCanFdTagRxOK && event.Tag != vectorCanFdTagTxOK {
		return true
	}
	msg := event.TagData.CanRxOkMsg
	dlc := msg.DLC
	payloadLen := dlcToLen(dlc)

	var unified UnifiedCANMessage
	unified.Direction = RX
	unified.ID = msg.CanID & 0x1FFFFFFF
	unified.DLC = dlc
	unified.IsFD = msg.MsgFlags&vectorCanFdRxFlagEDL != 0
	copy(unified.Data[:], msg.Data[:payloadLen])

	msgType := CAN
	if unified.IsFD {
		msgType = CANFD
	}
	logCANMessage("RX", unified.ID, unified.DLC, unified.Data[:payloadLen], msgType)

	select {
	case v.rxChan <- unified:
	default:
		log.Println("Warning: Vector receive channel full, dropping message")
	}

	return true
}

func (v *Vector) readOneCAN() bool {
	var event xlEvent
	eventCount := uint32(1)
	status, _, _ := v.receiveProc.Call(
		uintptr(v.portHandle),
		uintptr(unsafe.Pointer(&eventCount)),
		uintptr(unsafe.Pointer(&event)),
	)
	switch int32(status) {
	case vectorStatusSuccess:
	case vectorStatusQueueIsEmpty:
		return false
	default:
		log.Printf("Vector CAN receive error: %s", v.errorString(int16(status)))
		return false
	}

	if event.Tag != vectorEventTagReceiveMsg {
		return true
	}
	if event.TagData.Msg.Flags&vectorCanMsgFlagErrorFrame != 0 {
		return true
	}

	dlc := byte(event.TagData.Msg.DLC)
	payloadLen := dlcToLen(dlc)

	var unified UnifiedCANMessage
	unified.Direction = RX
	unified.ID = event.TagData.Msg.ID & 0x1FFFFFFF
	unified.DLC = dlc
	unified.IsFD = false
	copy(unified.Data[:], event.TagData.Msg.Data[:payloadLen])

	logCANMessage("RX", unified.ID, unified.DLC, unified.Data[:payloadLen], CAN)

	select {
	case v.rxChan <- unified:
	default:
		log.Println("Warning: Vector receive channel full, dropping message")
	}

	return true
}

func (v *Vector) callStatus(proc *syscall.LazyProc, args ...uintptr) error {
	if proc == nil {
		return errors.New("vector procedure not loaded")
	}
	status, _, _ := proc.Call(args...)
	if int32(status) != vectorStatusSuccess {
		return fmt.Errorf("%s", v.errorString(int16(status)))
	}
	return nil
}

func (v *Vector) errorString(status int16) string {
	if v.getErrorStringProc == nil {
		return fmt.Sprintf("status=%d", status)
	}
	ptr, _, _ := v.getErrorStringProc.Call(uintptr(status))
	if ptr == 0 {
		return fmt.Sprintf("status=%d", status)
	}
	return bytePtrToString((*byte)(unsafe.Pointer(ptr)))
}

func bytePtrToString(p *byte) string {
	if p == nil {
		return ""
	}
	buf := make([]byte, 0, 128)
	for *p != 0 {
		buf = append(buf, *p)
		p = (*byte)(unsafe.Pointer(uintptr(unsafe.Pointer(p)) + 1))
	}
	return string(buf)
}

func (v *Vector) cleanup(err error) error {
	v.Stop()
	return err
}
