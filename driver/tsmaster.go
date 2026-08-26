//go:build windows

package driver

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"runtime"
	"syscall"
	"time"

	"unsafe"

	"golang.org/x/sys/windows/registry"
)

const (
	BUS_UNKNOWN_TYPE = iota
	TS_TCP_DEVICE
	XL_USB_DEVICE
	TS_USB_DEVICE
	PEAK_USB_DEVICE
	KVASER_USB_DEVICE
	ZLG_USB_DEVICE
	ICS_USB_DEVICE
	TS_TC1005_DEVICE
	CANABLE_USB_DEVICE
	TS_WIRELESS_OBD
	TS_USB_DEVICE_EX
	IXXAT_USB_DEVICE
	TS_ETH_IF_DEVICE
	TS_USB_IF_DEVICE
	BUS_DEV_TYPE_COUNT
)

// TSMaster
const (
	TS_UNKNOWN_DEVICE = iota
	TSCAN_PRO
	TSCAN_Lite1
	TC1001
	TL1001
	TC1011
	TM5011
	TC1002
	TC1014
	TSCANFD2517
	TC1026
	TC1016
	TC1012
	TC1013
	TLog1002
	TC1034
	TC1018
	GW2116
	TC2115
	MP1013
	TC1113
	TC1114
	TP1013
	TC1017
	TP1018
	TF10XX
	TL1004_FD_4_LIN_2
	TE1051
	TP1051
	TP1034
	TTS9015
	TP1026
	TTS1026
	TTS1034
	TTS1018
	TL1011
	TTS1015_LiAuto
	TTS1013_LiAuto
	TTS1016Pro
	TC1054Pro
	TC1054
	TLog1038
	TO1013
	TC1034Pro
	TC1018Pro
	TC1038Pro
	TC1014Pro
	TC1034ProPlus
	TA1038
	TC1055Pro
	TC1056Pro
	TC1057Pro
	TC4016
	GW2208
	TLog1039
	GW1040
	TC3014
	TP1014
	TA825_4
	TC1013HV
	TC1052
	TTS1017Pro
	TLog1057
	TC1017Pro
	GW2202
	GW2204
	GW2212
	TA821
	TX1000
	TC1055ProPlus
	TC1043
	TS_DEV_END
)

// TSMasterMap 设备编号对照表
var TSMasterMap = map[string]int{
	"TS_UNKNOWN_DEVICE": TS_UNKNOWN_DEVICE,
	"TSCAN_PRO":         TSCAN_PRO,
	"TSCAN_Lite1":       TSCAN_Lite1,
	"TC1001":            TC1001,
	"TL1001":            TL1001,
	"TC1011":            TC1011,
	"TM5011":            TM5011,
	"TC1002":            TC1002,
	"TC1014":            TC1014,
	"TSCANFD2517":       TSCANFD2517,
	"TC1026":            TC1026,
	"TC1016":            TC1016,
	"TC1012":            TC1012,
	"TC1013":            TC1013,
	"TLog1002":          TLog1002,
	"TC1034":            TC1034,
	"TC1018":            TC1018,
	"GW2116":            GW2116,
	"TC2115":            TC2115,
	"MP1013":            MP1013,
	"TC1113":            TC1113,
	"TC1114":            TC1114,
	"TP1013":            TP1013,
	"TC1017":            TC1017,
	"TP1018":            TP1018,
	"TF10XX":            TF10XX,
	"TL1004_FD_4_LIN_2": TL1004_FD_4_LIN_2,
	"TE1051":            TE1051,
	"TP1051":            TP1051,
	"TP1034":            TP1034,
	"TTS9015":           TTS9015,
	"TP1026":            TP1026,
	"TTS1026":           TTS1026,
	"TTS1034":           TTS1034,
	"TTS1018":           TTS1018,
	"TL1011":            TL1011,
	"TTS1015_LiAuto":    TTS1015_LiAuto,
	"TTS1013_LiAuto":    TTS1013_LiAuto,
	"TTS1016Pro":        TTS1016Pro,
	"TC1054Pro":         TC1054Pro,
	"TC1054":            TC1054,
	"TLog1038":          TLog1038,
	"TO1013":            TO1013,
	"TC1034Pro":         TC1034Pro,
	"TC1018Pro":         TC1018Pro,
	"TC1038Pro":         TC1038Pro,
	"TC1014Pro":         TC1014Pro,
	"TC1034ProPlus":     TC1034ProPlus,
	"TA1038":            TA1038,
	"TC1055Pro":         TC1055Pro,
	"TC1056Pro":         TC1056Pro,
	"TC1057Pro":         TC1057Pro,
	"TC4016":            TC4016,
	"GW2208":            GW2208,
	"TLog1039":          TLog1039,
	"GW1040":            GW1040,
	"TC3014":            TC3014,
	"TP1014":            TP1014,
	"TA825_4":           TA825_4,
	"TC1013HV":          TC1013HV,
	"TC1052":            TC1052,
	"TTS1017Pro":        TTS1017Pro,
	"TLog1057":          TLog1057,
	"TC1017Pro":         TC1017Pro,
	"GW2202":            GW2202,
	"GW2204":            GW2204,
	"GW2212":            GW2212,
	"TA821":             TA821,
	"TX1000":            TX1000,
	"TC1055ProPlus":     TC1055ProPlus,
	"TC1043":            TC1043,
	"TS_DEV_END":        TS_DEV_END,
}

// deviceNameFromType 根据设备编号反查设备名称
func deviceNameFromType(deviceType int) (string, error) {
	for name, id := range TSMasterMap {
		if id == deviceType && name != "TS_UNKNOWN_DEVICE" && name != "TS_DEV_END" {
			return name, nil
		}
	}
	return "", fmt.Errorf("unsupported TSMaster device type: %d", deviceType)
}

type TSMasterLoader struct {
	DLL     *syscall.LazyDLL
	DLLPath string
}

// NewTSMasterLoader 创建新的TSMaster加载器
func NewTSMasterLoader() (*TSMasterLoader, error) {
	loader := &TSMasterLoader{}

	dllPath, err := getTSMasterDLLFromRegistry()
	if err != nil {
		return nil, fmt.Errorf("failed to get TSMaster DLL path from registry: %w", err)
	}
	if dllPath == "" {
		return nil, fmt.Errorf("TSMaster DLL path from registry is empty")
	}

	loader.DLLPath = dllPath
	loader.DLL = syscall.NewLazyDLL(dllPath)

	if err := loader.DLL.Load(); err != nil {
		return nil, fmt.Errorf("failed to load TSMaster.dll: %v", err)
	}

	return loader, nil
}

func getTSMasterDLLFromRegistry() (string, error) {
	regPath := `Software\TOSUN\TSMaster`
	key, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		regPath,
		registry.QUERY_VALUE|registry.WOW64_32KEY,
	)
	if err != nil {
		return "", err
	}
	defer key.Close()

	keyName := "libTSMaster_x64"
	if runtime.GOARCH == "386" {
		keyName = "libTSMaster_x86"
	}

	value, _, err := key.GetStringValue(keyName)
	return value, err
}

// GetProcAddress 获取函数地址
func (t *TSMasterLoader) GetProcAddress(procName string) *syscall.LazyProc {
	if t.DLL == nil {
		return nil
	}
	return t.DLL.NewProc(procName)
}

// Close 关闭DLL
func (t *TSMasterLoader) Close() error {
	// syscall.LazyDLL 没有显式的Close方法
	// Windows会在进程结束时自动清理
	t.DLL = nil
	return nil
}

type TLIBCAN struct {
	FIdxChn     uint8    // 通道
	FProperties uint8    // bit0 TX, bit1 remote, bit2 extended
	FDLC        uint8    // dlc from 0 to 8
	FReserved   uint8    // 保留字段
	FIdentifier int32    // ID
	FTimeUs     int64    // 时间戳
	FData       [8]uint8 // 报文数据
}
type TLIBCANFD struct {
	FIdxChn       uint8 // 通道
	FProperties   uint8 // bit0 TX, bit1 remote, bit2 extended
	FDLC          uint8 //dlc from 0 to 15
	FFDProperties uint8
	FIdentifier   int32     // ID
	FTimeUs       int64     // 时间戳
	FData         [64]uint8 // 报文数据
}

const (
	tsCANPropertyTX       = 1 << 0
	tsCANPropertyRemote   = 1 << 1
	tsCANPropertyExtended = 1 << 2
)

// TSMasterMapping maps one application-side logical channel to one physical
// channel on a TSMaster hardware device. All values are zero-based.
type TSMasterMapping struct {
	ApplicationChannel byte
	HardwareIndex      int
	HardwareChannel    byte
}

func DefaultTSMasterMapping(hardwareChannel byte) TSMasterMapping {
	return TSMasterMapping{
		ApplicationChannel: CHANNEL1,
		HardwareIndex:      0,
		HardwareChannel:    hardwareChannel,
	}
}

type TSMaster struct {
	driverObservability
	loader      *TSMasterLoader
	isConnected bool
	rxChan      chan CanFrame
	fanout      *rxFanout
	ctx         context.Context
	cancel      context.CancelFunc
	cfg         Config
	lifecycle   driverLifecycle
	canType     CanType
	// CANChannel is the physical hardware channel and is kept for backwards
	// compatibility. Internal send/receive operations use mapping.ApplicationChannel.
	CANChannel byte
	mapping    TSMasterMapping
	deviceType int
}

func NewTSMaster(cantype CanType, canChannel byte, deviceType int) *TSMaster {
	return NewTSMasterWithConfig(DefaultConfig(cantype, canChannel), deviceType)
}

func NewTSMasterWithConfig(cfg Config, deviceType int) *TSMaster {
	return NewTSMasterWithMapping(cfg, deviceType, DefaultTSMasterMapping(cfg.Channel))
}

func NewTSMasterWithMapping(cfg Config, deviceType int, mapping TSMasterMapping) *TSMaster {
	ctx, cancel := context.WithCancel(context.Background())
	cfg.Channel = mapping.HardwareChannel
	return &TSMaster{
		ctx:        ctx,
		cancel:     cancel,
		cfg:        cfg,
		canType:    cfg.Mode,
		CANChannel: mapping.HardwareChannel,
		mapping:    mapping,
		deviceType: deviceType,
	}
}

func (t *TSMaster) Init() error {
	t.lifecycle.opMu.Lock()
	defer t.lifecycle.opMu.Unlock()
	if t.lifecycle.isInitialized() {
		return nil
	}
	fmt.Println("=== TSMaster Initializing ===")

	cfg, err := normalizeConfig(t.cfg)
	if err != nil {
		return err
	}
	t.cfg = cfg
	t.canType = cfg.Mode
	t.CANChannel = t.mapping.HardwareChannel
	t.cfg.Channel = t.mapping.HardwareChannel
	if t.mapping.ApplicationChannel >= 32 {
		return fmt.Errorf("TSMaster application channel %d out of range (0-31)", t.mapping.ApplicationChannel)
	}
	if t.mapping.HardwareIndex < 0 {
		return fmt.Errorf("TSMaster hardware index must be >= 0: %d", t.mapping.HardwareIndex)
	}

	// 创建context和cancel函数
	t.ctx, t.cancel = context.WithCancel(context.Background())

	// 初始化接收通道
	t.rxChan = make(chan CanFrame, cfg.RxBufferSize)
	t.fanout = newRxFanout(t.ctx, t.rxChan, t.resetTelemetry())

	cleanup := func(err error) error {
		if t.cancel != nil {
			t.cancel()
		}
		if t.loader != nil && t.isConnected {
			_, _, _ = t.loader.GetProcAddress("tsapp_disconnect").Call()
		}
		if t.loader != nil {
			t.loader.Close()
			t.loader = nil
		}
		if t.fanout != nil {
			t.fanout.Close()
			t.fanout = nil
		}
		if t.rxChan != nil {
			close(t.rxChan)
			t.rxChan = nil
		}
		t.closeTelemetry()
		t.isConnected = false
		return err
	}

	// 创建TSMaster加载器
	t.loader, err = NewTSMasterLoader()
	if err != nil {
		return cleanup(fmt.Errorf("failed to load TSMaster DLL: %w", err))
	}

	fmt.Printf("✅ Successfully loaded TSMaster DLL\n")
	fmt.Printf("📁 DLL Path: %s\n", t.loader.DLLPath)

	// 初始化TSMaster库
	initialize_lib_tsmaster := t.loader.GetProcAddress("initialize_lib_tsmaster")
	appName, _ := syscall.UTF16PtrFromString("TSMaster_Go_Demo")
	r, _, _ := initialize_lib_tsmaster.Call(uintptr(unsafe.Pointer(appName)))
	fmt.Printf("Initialization result: %d\n", r)
	if r != 0 {
		return cleanup(fmt.Errorf("initialize_lib_tsmaster failed: %d", r))
	}

	// 枚举硬件设备
	var findDevice int32 = 0
	r, _, _ = t.loader.GetProcAddress("tsapp_enumerate_hw_devices").Call(uintptr(unsafe.Pointer(&findDevice)))
	fmt.Printf("Found devices: %d\n", findDevice)
	if r != 0 {
		return cleanup(fmt.Errorf("tsapp_enumerate_hw_devices failed: %d", r))
	}
	if findDevice <= 0 {
		return cleanup(errors.New("no TSMaster devices found"))
	}
	if t.mapping.HardwareIndex >= int(findDevice) {
		return cleanup(fmt.Errorf("TSMaster hardware index %d out of range; found %d device(s)", t.mapping.HardwareIndex, findDevice))
	}
	HardwareName, _ := syscall.BytePtrFromString("Hardware")
	r, _, _ = t.loader.GetProcAddress("tsapp_show_tsmaster_window").Call(uintptr(unsafe.Pointer(HardwareName)), uintptr(1))
	fmt.Printf("tsapp_show_tsmaster_window: %d\n", r)
	// 设置CAN通道数量
	channelCount := uintptr(t.mapping.ApplicationChannel) + 1
	r, _, _ = t.loader.GetProcAddress("tsapp_set_can_channel_count").Call(channelCount)
	fmt.Printf("Set CAN channel count result: %d\n", r)
	if r != 0 {
		return cleanup(fmt.Errorf("set CAN channel count failed: %d", r))
	}
	devName, err := deviceNameFromType(t.deviceType)
	if err != nil {
		return cleanup(err)
	}
	deviceName, _ := syscall.UTF16PtrFromString(devName)
	// TSAPI(s32)tsapp_set_mapping_verbose(
	// const char* AAppName,
	// const TLIBApplicationChannelType AAppChannelType,
	// const s32 AAppChannel,
	// const char* AHardwareName,
	// const TLIBBusToolDeviceType AHardwareType,
	// const s32 AHardwareSubType,
	// const s32 AHardwareIndex,
	// const s32 AHardwareChannel,
	// const bool AEnableMapping);
	// Map the logical application channel to the selected device/channel.
	r, _, _ = t.loader.GetProcAddress("tsapp_set_mapping_verbose").Call(
		uintptr(unsafe.Pointer(appName)),
		uintptr(0),                            // APP_CAN
		uintptr(t.mapping.ApplicationChannel), // 应用逻辑通道
		uintptr(unsafe.Pointer(deviceName)),
		uintptr(TS_USB_DEVICE),             // TS_USB_DEVICE
		uintptr(t.deviceType),              // 设备子类型
		uintptr(t.mapping.HardwareIndex),   // 硬件设备索引
		uintptr(t.mapping.HardwareChannel), // 硬件物理通道
		uintptr(1),                         // 启用映射
	)
	fmt.Printf(
		"Set mapping verbose (%s/%d app=%d hardware=%d:%d) result: %d\n",
		devName,
		t.deviceType,
		t.mapping.ApplicationChannel,
		t.mapping.HardwareIndex,
		t.mapping.HardwareChannel,
		r,
	)
	if r != 0 {
		return cleanup(fmt.Errorf("tsapp_set_mapping_verbose failed: %d", r))
	}
	br := float32(t.cfg.NominalBitrate) / 1000
	if t.canType == CANFD {
		bd := float32(t.cfg.DataBitrate) / 1000
		r, _, _ = t.loader.GetProcAddress("tsapp_configure_baudrate_canfd").Call(
			uintptr(t.mapping.ApplicationChannel),
			uintptr(math.Float32bits(br)),
			uintptr(math.Float32bits(bd)),
			uintptr(1),
			uintptr(0),
			uintptr(1),
		)
		fmt.Printf("CAN-FD bitrate configuration result: %d\n", r)
	} else {
		r, _, _ = t.loader.GetProcAddress("tsapp_configure_baudrate_can").Call(
			uintptr(t.mapping.ApplicationChannel),
			uintptr(math.Float32bits(br)),
			uintptr(0),
			uintptr(1),
		)
		fmt.Printf("CAN bitrate configuration result: %d\n", r)
	}
	if r != 0 {
		return cleanup(fmt.Errorf("configure bitrate failed: %d", r))
	}
	// 连接设备
	r, _, _ = t.loader.GetProcAddress("tsapp_connect").Call()
	fmt.Printf("Connect result: %d\n", r)
	if r != 0 {
		return cleanup(fmt.Errorf("tsapp_connect failed: %d", r))
	}
	t.isConnected = true

	// 启用接收FIFO
	enableFIFOProc := t.loader.GetProcAddress("tsfifo_enable_receive_fifo")
	if enableFIFOProc == nil {
		return cleanup(errors.New("tsfifo_enable_receive_fifo not found"))
	}
	if err := enableFIFOProc.Find(); err != nil {
		return cleanup(fmt.Errorf("tsfifo_enable_receive_fifo not found: %w", err))
	}
	// TSMaster.h declares this API as void. syscall.Proc.Call's first return
	// value is undefined for void functions (often 1), so it is not a status.
	enableFIFOProc.Call()

	t.lifecycle.markInitialized()
	return nil
}

func (t *TSMaster) Start() {
	if err := t.StartWithError(); err != nil {
		log.Printf("TSMaster start failed: %v", err)
	}
}

func (t *TSMaster) StartWithError() error {
	t.lifecycle.opMu.Lock()
	defer t.lifecycle.opMu.Unlock()
	if !t.lifecycle.isInitialized() || !t.isConnected {
		return fmt.Errorf("%w: TSMaster", ErrDriverNotInitialized)
	}
	if t.lifecycle.start(t.readLoop) {
		fmt.Println("TSMaster started")
	}
	return nil
}

func (t *TSMaster) readLoop() {
	ticker := time.NewTicker(t.cfg.PollingInterval)
	defer ticker.Stop()
	var canfdMsg [MsgBufferSize]TLIBCANFD
	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			var size = int32(MsgBufferSize)
			rxTxMode := uintptr(0)
			if t.cfg.IncludeTxEcho {
				rxTxMode = 1
			}
			if r, _, _ := t.loader.GetProcAddress("tsfifo_receive_canfd_msgs").Call(
				uintptr(unsafe.Pointer(&canfdMsg[0])),
				uintptr(unsafe.Pointer(&size)),
				uintptr(t.mapping.ApplicationChannel),
				rxTxMode,
			); r != 0 {
				continue
			}
			if size < 0 || size > MsgBufferSize {
				log.Printf("TSMaster returned invalid receive count %d", size)
				continue
			}
			for i := 0; i < int(size); i++ {
				msg := canfdMsg[i]
				if msg.FIdentifier < 0 || msg.FIdentifier > 0x7FF {
					continue
				}
				if msg.FProperties&(tsCANPropertyRemote|tsCANPropertyExtended) != 0 {
					continue
				}
				actualLen := msg.FDLC
				if actualLen > 15 {
					log.Printf("TSMaster returned invalid DLC %d", actualLen)
					continue
				}

				var unifiedMsg CanFrame
				// 使用统一的日志函数
				msgType := t.canType
				if msg.FFDProperties&1 == 0 {
					msgType = CAN
				} else {
					msgType = CANFD
				}
				switch canfdMsg[i].FProperties & tsCANPropertyTX {
				case 0:
					unifiedMsg = CanFrame{
						Direction: RX, ID: uint32(msg.FIdentifier), DLC: msg.FDLC, Data: msg.FData, IsFD: msg.FFDProperties&1 == 1,
					}

					logCANMessage("RX", unifiedMsg.ID, unifiedMsg.DLC, unifiedMsg.Data[:dlcToLen(unifiedMsg.DLC)], msgType)
				case 1:
					if !t.cfg.IncludeTxEcho {
						continue
					}
					unifiedMsg = CanFrame{
						Direction: TX, ID: uint32(msg.FIdentifier), DLC: msg.FDLC, Data: msg.FData, IsFD: msg.FFDProperties&1 == 1,
					}
					logCANMessage("TX", unifiedMsg.ID, unifiedMsg.DLC, unifiedMsg.Data[:dlcToLen(unifiedMsg.DLC)], msgType)
				}

				t.publishRx(t.ctx, t.rxChan, unifiedMsg)
			}
		}
	}
}

func (t *TSMaster) Stop() {
	t.lifecycle.opMu.Lock()
	defer t.lifecycle.opMu.Unlock()
	wasInitialized := t.lifecycle.cancelAndWait(t.cancel)

	if t.fanout != nil {
		t.fanout.Close()
		t.fanout = nil
	}

	if wasInitialized && t.loader != nil && t.isConnected {
		r, _, _ := t.loader.GetProcAddress("tsapp_disconnect").Call()
		fmt.Printf("Disconnect result: %d\n", r)
		t.isConnected = false
	}

	if t.loader != nil {
		t.loader.Close()
		t.loader = nil
	}

	if t.rxChan != nil {
		close(t.rxChan)
		t.rxChan = nil
	}
	t.closeTelemetry()
	fmt.Println("TSMaster stopped")
}

func (t *TSMaster) Write(id int32, fd bool, data []byte) error {
	t.lifecycle.opMu.Lock()
	defer t.lifecycle.opMu.Unlock()
	if !t.lifecycle.isInitialized() {
		return errors.New("TSMaster driver is not initialized")
	}
	if err := validateWrite(t.cfg, id, fd, data); err != nil {
		return err
	}

	var canfdMsg TLIBCANFD
	canfdMsg.FIdxChn = t.mapping.ApplicationChannel
	canfdMsg.FIdentifier = id
	canfdMsg.FProperties = 1
	canfdMsg.FDLC = dataLenToDlc(len(data))
	if fd {
		canfdMsg.FFDProperties = uint8(CANFD)
	} else {
		canfdMsg.FFDProperties = uint8(CAN)
	}
	// 复制数据到CAN消息
	maxLen := dlcToLen(canfdMsg.FDLC)
	for i := 0; i < maxLen && i < len(data); i++ {
		canfdMsg.FData[i] = data[i]
	}
	if r, _, _ := t.loader.GetProcAddress("tsapp_transmit_canfd_async").Call(uintptr(unsafe.Pointer(&canfdMsg))); r != 0 {
		return fmt.Errorf("failed to send CAN-FD message, result code: %d", r)
	}
	return nil
}

func (t *TSMaster) RxChan() <-chan CanFrame {
	t.lifecycle.opMu.Lock()
	defer t.lifecycle.opMu.Unlock()
	if t.fanout == nil {
		return nil
	}
	ch, _ := t.fanout.Subscribe(t.cfg.RxBufferSize)
	return ch
}

func (t *TSMaster) SubscribeRx(buffer int) (<-chan CanFrame, func()) {
	t.lifecycle.opMu.Lock()
	defer t.lifecycle.opMu.Unlock()
	if t.fanout == nil {
		return nil, func() {}
	}
	return t.fanout.Subscribe(buffer)
}

func (t *TSMaster) IsFDMode() bool {
	t.lifecycle.opMu.Lock()
	defer t.lifecycle.opMu.Unlock()
	return t.canType == CANFD
}

func (t *TSMaster) Config() Config {
	t.lifecycle.opMu.Lock()
	defer t.lifecycle.opMu.Unlock()
	return t.cfg
}

func (t *TSMaster) Mapping() TSMasterMapping {
	t.lifecycle.opMu.Lock()
	defer t.lifecycle.opMu.Unlock()
	return t.mapping
}
