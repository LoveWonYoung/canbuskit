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

// findTSMasterDLL 查找TSMaster DLL文件路径
func findTSMasterDLL() (string, error) {
	var dllPath string
	basePath := "C:\\Program Files (x86)\\TOSUN\\TSMaster"

	// 定义注册表路径
	tsmasterLocation := `Software\TOSUN\TSMaster`

	// 获取系统架构
	arch := runtime.GOARCH

	// 尝试从注册表获取DLL路径
	if path, err := getDLLPathFromRegistry(tsmasterLocation, arch); err == nil && path != "" {
		dllPath = filepath.Join(filepath.Dir(path), "TSMaster.dll")
		if _, err := os.Stat(dllPath); err == nil {
			return dllPath, nil
		}
	}

	// 如果注册表中没有找到，使用默认路径
	if arch == "386" {
		dllPath = filepath.Join(basePath, "bin", "TSMaster.dll")
	} else {
		dllPath = filepath.Join(basePath, "bin64", "TSMaster.dll")
	}

	// 检查文件是否存在
	if _, err := os.Stat(dllPath); err != nil {
		return "", fmt.Errorf("could not find TSMaster.dll at '%s': %v", dllPath, err)
	}

	return dllPath, nil
}

// getDLLPathFromRegistry 从注册表获取DLL路径
func getDLLPathFromRegistry(regPath, arch string) (string, error) {
	// 打开注册表键
	key, err := registry.OpenKey(registry.CURRENT_USER, regPath, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer key.Close()

	// 根据架构确定要查找的键名
	var keyName string
	if arch == "386" {
		keyName = "libTSMaster_x86"
	} else {
		keyName = "libTSMaster_x64"
	}

	// 读取键值
	value, _, err := key.GetStringValue(keyName)
	if err != nil {
		return "", err
	}

	return value, nil
}

// loadTSMasterDLL 加载TSMaster DLL
func loadTSMasterDLL() (*syscall.LazyDLL, error) {
	dllPath, err := findTSMasterDLL()
	if err != nil {
		return nil, err
	}

	// 加载DLL
	dll := syscall.NewLazyDLL(dllPath)
	if err := dll.Load(); err != nil {
		return nil, fmt.Errorf("failed to load TSMaster.dll from '%s': %v", dllPath, err)
	}

	fmt.Printf("Successfully loaded TSMaster.dll from: %s\n", dllPath)
	return dll, nil
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
	} else {
		fmt.Println("not find dll ", dllPath)
	}

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
	ctx         context.Context
	cancel      context.CancelFunc
	canType     CanType
}

func NewTSMaster(cantype CanType) *TSMaster {
	ctx, cancel := context.WithCancel(context.Background())
	return &TSMaster{
		rxChan:  make(chan UnifiedCANMessage, RxChannelBufferSize),
		ctx:     ctx,
		cancel:  cancel,
		canType: cantype,
	}
}

func (t *TSMaster) Init() error {
	fmt.Println("=== TSMaster Initializing ===")

	// 创建context和cancel函数
	t.ctx, t.cancel = context.WithCancel(context.Background())

	// 初始化接收通道
	t.rxChan = make(chan UnifiedCANMessage, RxChannelBufferSize)

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

	// 设置CAN通道数量
	r, _, _ = t.loader.GetProcAddress("tsapp_set_can_channel_count").Call(uintptr(1))
	fmt.Printf("Set CAN channel count result: %d\n", r)

	// 设置映射
	r, _, _ = t.loader.GetProcAddress("tsapp_set_mapping_verbose").Call(
		uintptr(unsafe.Pointer(appName)),
		uintptr(0),
		uintptr(0),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("TC1016"))),
		uintptr(3),
		uintptr(11),
		uintptr(0),
		uintptr(0),
		uintptr(1), // True
	)
	fmt.Printf("Set mapping verbose result: %d\n", r)
	//tsapp_configure_baudrate_canfd(0, 500.0, 2000.0, 1, 0, True):
	br := float32(500.0)
	bd := float32(2000.0)
	r, _, _ = t.loader.GetProcAddress("tsapp_configure_baudrate_canfd").Call(
		uintptr(0),
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
				uintptr(0),
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
				unifiedMsg := UnifiedCANMessage{
					ID: uint32(msg.FIdentifier), DLC: msg.FDLC, Data: msg.FData, IsFD: msg.FFDProperties == 1,
				}
				// 使用统一的日志函数
				msgType := t.canType
				if msg.FFDProperties == 0 {
					msgType = CAN
				} else {
					msgType = CANFD
				}
				if canfdMsg[i].FProperties&1 == 1 {
					logCANMessage("TX", unifiedMsg.ID, unifiedMsg.DLC, unifiedMsg.Data[:dlcToLen(unifiedMsg.DLC)], msgType)
				} else if canfdMsg[i].FProperties&1 == 0 {
					logCANMessage("RX", unifiedMsg.ID, unifiedMsg.DLC, unifiedMsg.Data[:dlcToLen(unifiedMsg.DLC)], msgType)
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
func (t *TSMaster) Write(id int32, data []byte) error {
	var canfdMsg TLIBCANFD
	canfdMsg.FIdxChn = 0
	canfdMsg.FIdentifier = id
	canfdMsg.FProperties = 1
	canfdMsg.FDLC = dataLenToDlc(len(data))
	if len(data) < 8 {
		canfdMsg.FDLC = 8
	}
	canfdMsg.FFDProperties = uint8(t.canType)
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
	return t.rxChan
}
func (t *TSMaster) Context() context.Context {
	if t.ctx != nil {
		return t.ctx
	}
	return context.Background()
}
