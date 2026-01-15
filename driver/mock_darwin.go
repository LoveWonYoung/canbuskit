//go:build darwin

package driver

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// MockCanMix 是 Darwin 系统上的虚拟 CAN 驱动实现
// 用于开发和测试，不依赖实际硬件
type MockCanMix struct {
	mu        sync.Mutex
	rxChan    chan UnifiedCANMessage
	ctx       context.Context
	cancel    context.CancelFunc
	canType   CanType
	running   bool
	writeLog  []WriteRecord     // 记录写入的数据
	responses []MockCANResponse // 预设的自动响应
}

// WriteRecord 记录一次写入操作
type WriteRecord struct {
	ID        int32
	Data      []byte
	Timestamp time.Time
}

// MockCANResponse 定义预设的自动响应
type MockCANResponse struct {
	TriggerID   uint32        // 触发响应的请求 ID
	ResponseID  uint32        // 响应的 ID
	TriggerData []byte        // 触发响应的数据前缀 (可选)
	Response    []byte        // 响应数据
	Delay       time.Duration // 响应延迟
}

// CanType 定义 CAN 类型 (与 Windows 版本保持一致)
type CanType byte

const (
	CAN   CanType = 0
	CANFD CanType = 1
)

// 缓冲区配置常量
const (
	RxChannelBufferSize = 1024
	MsgBufferSize       = 1024
	PollingInterval     = time.Millisecond
	InitDelay           = 20 * time.Millisecond
)

// NewCanMix 创建一个新的虚拟 CAN 设备实例
func NewCanMix(canType CanType) *MockCanMix {
	ctx, cancel := context.WithCancel(context.Background())
	return &MockCanMix{
		rxChan:  make(chan UnifiedCANMessage, RxChannelBufferSize),
		ctx:     ctx,
		cancel:  cancel,
		canType: canType,
	}
}

// Init 初始化虚拟设备 (总是成功)
func (c *MockCanMix) Init() error {
	log.Println("[Mock] CAN 设备初始化成功 (虚拟模式)")
	return nil
}

// Start 启动虚拟设备
func (c *MockCanMix) Start() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return
	}
	c.running = true
	log.Println("[Mock] CAN 设备已启动 (虚拟模式)")
}

// Stop 停止虚拟设备
func (c *MockCanMix) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return
	}
	c.running = false
	c.cancel()
	close(c.rxChan)
	log.Println("[Mock] CAN 设备已停止")
}

// Write 写入数据到虚拟设备
func (c *MockCanMix) Write(id int32, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return fmt.Errorf("设备未启动")
	}

	// 记录写入
	c.writeLog = append(c.writeLog, WriteRecord{
		ID:        id,
		Data:      append([]byte{}, data...), // 复制数据
		Timestamp: time.Now(),
	})

	// 日志输出
	typeStr := "CAN"
	if c.canType == CANFD {
		typeStr = "CANFD"
	}
	log.Printf("[Mock] TX %s: ID=0x%03X, DLC=%02d, Data=% 02X", typeStr, id, len(data), data)

	// 检查预设响应
	for _, resp := range c.responses {
		if resp.TriggerID == uint32(id) {
			// 检查数据前缀匹配 (如果设置了)
			if len(resp.TriggerData) > 0 {
				if len(data) < len(resp.TriggerData) {
					continue
				}
				match := true
				for i, b := range resp.TriggerData {
					if data[i] != b {
						match = false
						break
					}
				}
				if !match {
					continue
				}
			}

			// 发送响应
			go func(r MockCANResponse) {
				time.Sleep(r.Delay)
				c.InjectMessage(r.ResponseID, r.Response)
			}(resp)
		}
	}

	return nil
}

// RxChan 返回接收通道
func (c *MockCanMix) RxChan() <-chan UnifiedCANMessage {
	return c.rxChan
}

// Context 返回设备上下文
func (c *MockCanMix) Context() context.Context {
	return c.ctx
}

// ============================================================================
// Mock 专用方法 - 用于测试
// ============================================================================

// InjectMessage 向接收通道注入一条消息 (模拟接收)
func (c *MockCanMix) InjectMessage(id uint32, data []byte) error {
	c.mu.Lock()
	running := c.running
	c.mu.Unlock()

	if !running {
		return fmt.Errorf("设备未启动")
	}

	var dataArr [64]byte
	copy(dataArr[:], data)

	msg := UnifiedCANMessage{
		ID:   id,
		DLC:  byte(len(data)),
		Data: dataArr,
		IsFD: c.canType == CANFD,
	}

	select {
	case c.rxChan <- msg:
		typeStr := "CAN"
		if c.canType == CANFD {
			typeStr = "CANFD"
		}
		log.Printf("[Mock] RX %s: ID=0x%03X, DLC=%02d, Data=% 02X", typeStr, id, len(data), data)
		return nil
	default:
		return fmt.Errorf("接收通道已满")
	}
}

// SetResponses 设置预设的自动响应
func (c *MockCanMix) SetResponses(responses ...MockCANResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.responses = responses
}

// AddResponse 添加一个预设响应
func (c *MockCanMix) AddResponse(triggerID, responseID uint32, triggerData, response []byte, delay time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.responses = append(c.responses, MockCANResponse{
		TriggerID:   triggerID,
		ResponseID:  responseID,
		TriggerData: triggerData,
		Response:    response,
		Delay:       delay,
	})
}

// ClearResponses 清除所有预设响应
func (c *MockCanMix) ClearResponses() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.responses = nil
}

// GetWriteLog 获取写入日志
func (c *MockCanMix) GetWriteLog() []WriteRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]WriteRecord{}, c.writeLog...)
}

// ClearWriteLog 清除写入日志
func (c *MockCanMix) ClearWriteLog() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writeLog = nil
}

// IsRunning 检查设备是否正在运行
func (c *MockCanMix) IsRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

// ============================================================================
// USB 模拟函数 (空实现，保持接口兼容)
// ============================================================================

var (
	DevHandle [10]int
	DEVIndex  = 0
)

func UsbScan() bool {
	log.Println("[Mock] USB 扫描 (虚拟)")
	return true
}

func UsbOpen() bool {
	log.Println("[Mock] USB 打开 (虚拟)")
	return true
}

func UsbClose() bool {
	log.Println("[Mock] USB 关闭 (虚拟)")
	return true
}
