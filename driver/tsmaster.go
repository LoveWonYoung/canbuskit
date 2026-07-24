//go:build windows

package driver

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	// "time"
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

	dllPath, err := loader.findDLLPath()
	if err != nil {
		return nil, err
	}

	loader.DLLPath = dllPath
	loader.DLL = syscall.NewLazyDLL(dllPath)

	if err := loader.DLL.Load(); err != nil {
		return nil, fmt.Errorf("failed to load TSMaster.dll: %v", err)
	}

	return loader, nil
}

// findDLLPath 查找DLL文件路径
func (t *TSMasterLoader) findDLLPath() (string, error) {
	// 1. 从默认安装目录查找
	basePath := "C:\\Program Files (x86)\\TOSUN\\TSMaster"

	var dllPath string
	if runtime.GOARCH == "386" {
		dllPath = filepath.Join(basePath, "bin", "TSMaster.dll")
	} else {
		dllPath = filepath.Join(basePath, "bin64", "TSMaster.dll")
	}

	if t.fileExists(dllPath) {
		fmt.Println("find dll ", dllPath)
		return dllPath, nil
	}

	fmt.Println("not find dll ", dllPath)

	// 2. 如果当前路径未找到，再从注册表获取
	if path, err := t.getDLLFromRegistry(); err == nil && path != "" {
		dllPath = filepath.Join(filepath.Dir(path), "TSMaster.dll")
		if t.fileExists(dllPath) {
			return dllPath, nil
		}
	}

	return "", fmt.Errorf("TSMaster.dll not found in default or registry paths")
}

// getDLLFromRegistry 从注册表获取DLL路径
func (t *TSMasterLoader) getDLLFromRegistry() (string, error) {
	regPath := `Software\TOSUN\TSMaster`

	key, err := registry.OpenKey(registry.CURRENT_USER, regPath, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer key.Close()

	var keyName string
	if runtime.GOARCH == "386" {
		keyName = "libTSMaster_x86"
	} else {
		keyName = "libTSMaster_x64"
	}

	value, _, err := key.GetStringValue(keyName)
	return value, err
}

// fileExists 检查文件是否存在
func (t *TSMasterLoader) fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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
	FProperties uint8    // 属性定义：[7] 0-normal frame, 1-error frame
	FDLC        uint8    // dlc from 0 to 8
	FReserved   uint8    // 保留字段
	FIdentifier int32    // ID
	FTimeUs     int64    // 时间戳
	FData       [8]uint8 // 报文数据
}
type TLIBCANFD struct {
	FIdxChn       uint8 // 通道
	FProperties   uint8 // 属性定义：[7] 0-normal frame, 1-error frame
	FDLC          uint8 //dlc from 0 to 15
	FFDProperties uint8
	FIdentifier   int32     // ID
	FTimeUs       int64     // 时间戳
	FData         [64]uint8 // 报文数据
}

type TSMaster struct {
	loader      *TSMasterLoader
	isConnected bool
	rxChan      chan UnifiedCANMessage
	fanout      *rxFanout
	ctx         context.Context
	cancel      context.CancelFunc
	canType     CanType
	CANChannel  byte
	deviceType  int
}

func NewTSMaster(cantype CanType, canChannel byte, deviceType int) *TSMaster {
	ctx, cancel := context.WithCancel(context.Background())
	return &TSMaster{
		rxChan:     make(chan UnifiedCANMessage, RxChannelBufferSize),
		ctx:        ctx,
		cancel:     cancel,
		canType:    cantype,
		CANChannel: canChannel,
		deviceType: deviceType,
	}
}

func (t *TSMaster) Init() error {
	fmt.Println("=== TSMaster Initializing ===")

	// 创建context和cancel函数
	t.ctx, t.cancel = context.WithCancel(context.Background())

	// 初始化接收通道
	t.rxChan = make(chan UnifiedCANMessage, RxChannelBufferSize)
	t.fanout = newRxFanout(t.ctx, t.rxChan)

	cleanup := func(err error) error {
		if t.cancel != nil {
			t.cancel()
		}
		if t.loader != nil {
			t.loader.Close()
			t.loader = nil
		}
		if t.rxChan != nil {
			close(t.rxChan)
			t.rxChan = nil
		}
		t.isConnected = false
		return err
	}

	// 创建TSMaster加载器
	var err error
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
	HardwareName, _ := syscall.BytePtrFromString("Hardware")
	r, _, _ = t.loader.GetProcAddress("tsapp_show_tsmaster_window").Call(uintptr(unsafe.Pointer(HardwareName)), uintptr(1))
	fmt.Printf("tsapp_show_tsmaster_window: %d\n", r)
	// 设置CAN通道数量
	r, _, _ = t.loader.GetProcAddress("tsapp_set_can_channel_count").Call(uintptr(4))
	fmt.Printf("Set CAN channel count result: %d\n", r)
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
	// 设置映射: APP_CAN -> TS_USB_DEVICE(3) / deviceSubType / CANChannel
	r, _, _ = t.loader.GetProcAddress("tsapp_set_mapping_verbose").Call(
		uintptr(unsafe.Pointer(appName)),
		uintptr(0), // APP_CAN
		uintptr(0), // 软件通道
		uintptr(unsafe.Pointer(deviceName)),
		uintptr(TS_USB_DEVICE), // TS_USB_DEVICE
		uintptr(t.deviceType),  // 设备子类型
		uintptr(0),             // 硬件索引
		uintptr(0),             // 硬件通道
		uintptr(1),             // 启用映射
	)
	fmt.Printf("Set mapping verbose (%s/%d) result: %d\n", devName, t.deviceType, r)
	if r != 0 {
		return cleanup(fmt.Errorf("tsapp_set_mapping_verbose failed: %d", r))
	}
	//tsapp_configure_baudrate_canfd(0, 500.0, 2000.0, 1, 0, True):
	br := float32(500.0)
	bd := float32(2000.0)
	r, _, _ = t.loader.GetProcAddress("tsapp_configure_baudrate_canfd").Call(
		uintptr(t.CANChannel),
		uintptr(math.Float32bits(br)),
		uintptr(math.Float32bits(bd)),
		uintptr(1),
		uintptr(0),
		uintptr(1),
	)
	fmt.Printf("canfd init: %d\n", r)
	// 连接设备
	r, _, _ = t.loader.GetProcAddress("tsapp_connect").Call()
	fmt.Printf("Connect result: %d\n", r)
	if r != 0 {
		return cleanup(fmt.Errorf("tsapp_connect failed: %d", r))
	}
	t.isConnected = true

	// 启用接收FIFO
	r, _, _ = t.loader.GetProcAddress("tsfifo_enable_receive_fifo").Call()
	fmt.Printf("Enable receive FIFO result: %d\n", r)

	return nil
}
func (t *TSMaster) Start() {
	if !t.isConnected {
		fmt.Println("TSMaster not connected, cannot start")
		return
	}
	fmt.Println("TSMaster started")
	// 这里可以启动接收线程等
	go t.readLoop()
}
func (t *TSMaster) readLoop() {
	ticker := time.NewTicker(PollingInterval)
	defer ticker.Stop()
	var canfdMsg [MsgBufferSize]TLIBCANFD
	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			var size = int32(MsgBufferSize)
			if r, _, _ := t.loader.GetProcAddress("tsfifo_receive_canfd_msgs").Call(
				uintptr(unsafe.Pointer(&canfdMsg[0])),
				uintptr(unsafe.Pointer(&size)),
				uintptr(t.CANChannel),
				uintptr(1),
			); r != 0 {
				continue
			}
			for i := 0; i < int(size); i++ {
				msg := canfdMsg[i]
				actualLen := msg.FDLC
				if actualLen == 0 {
					continue
				}

				var unifiedMsg UnifiedCANMessage
				// 使用统一的日志函数
				msgType := t.canType
				if msg.FFDProperties&1 == 0 {
					msgType = CAN
				} else {
					msgType = CANFD
				}
				switch canfdMsg[i].FProperties & 1 {
				case 0:
					unifiedMsg = UnifiedCANMessage{
						Direction: RX, ID: uint32(msg.FIdentifier), DLC: msg.FDLC, Data: msg.FData, IsFD: msg.FFDProperties&1 == 1,
					}

					logCANMessage("RX", unifiedMsg.ID, unifiedMsg.DLC, unifiedMsg.Data[:dlcToLen(unifiedMsg.DLC)], msgType)
				case 1:
					unifiedMsg = UnifiedCANMessage{
						Direction: TX, ID: uint32(msg.FIdentifier), DLC: msg.FDLC, Data: msg.FData, IsFD: msg.FFDProperties&1 == 1,
					}
					logCANMessage("TX", unifiedMsg.ID, unifiedMsg.DLC, unifiedMsg.Data[:dlcToLen(unifiedMsg.DLC)], msgType)
				}

				select {
				case t.rxChan <- unifiedMsg:
				default:
					log.Println("警告: 驱动接收channel(FD)已满，消息被丢弃")
				}
			}
		}
	}
}
func (t *TSMaster) Stop() {
	if t.cancel != nil {
		t.cancel()
	}

	if t.fanout != nil {
		t.fanout.Close()
	}

	if t.loader != nil && t.isConnected {
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

	fmt.Println("TSMaster stopped")
}
func (t *TSMaster) Write(id int32, fd bool, data []byte) error {
	if len(data) == 0 {
		return errors.New("data length is 0")
	}
	if !fd && len(data) > 8 {
		return fmt.Errorf("data length %d exceeds CAN maximum of 8", len(data))
	}
	if fd && len(data) > 64 {
		return fmt.Errorf("data length %d exceeds CAN-FD maximum of 64", len(data))
	}

	var canfdMsg TLIBCANFD
	canfdMsg.FIdxChn = t.CANChannel
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
func (t *TSMaster) RxChan() <-chan UnifiedCANMessage {
	if t.fanout == nil {
		return nil
	}
	return t.fanout.Subscribe(RxChannelBufferSize)
}
func (t *TSMaster) Context() context.Context {
	if t.ctx != nil {
		return t.ctx
	}
	return context.Background()
}

func (t *TSMaster) IsFDMode() bool {
	return t.canType == CANFD
}
