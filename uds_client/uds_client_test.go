package uds_client

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/LoveWonYoung/canbuskit/driver"
	isotp "github.com/LoveWonYoung/canbuskit/tp_layer"
)

// ============================================================================
// Mock 实现
// ============================================================================

// MockCANDriver 是 CANDriver 接口的 Mock 实现
type MockCANDriver struct {
	mu        sync.Mutex
	rxChan    chan driver.UnifiedCANMessage
	ctx       context.Context
	cancel    context.CancelFunc
	writeLog  [][]byte       // 记录所有写入的数据
	responses []MockResponse // 预设的响应
	respIndex int
	initErr   error // Init() 返回的错误
}

// MockResponse 定义一个预设的响应
type MockResponse struct {
	Delay time.Duration // 响应延迟
	Data  []byte        // 响应数据
}

func NewMockCANDriver() *MockCANDriver {
	ctx, cancel := context.WithCancel(context.Background())
	return &MockCANDriver{
		rxChan: make(chan driver.UnifiedCANMessage, 100),
		ctx:    ctx,
		cancel: cancel,
	}
}

func (m *MockCANDriver) Init() error                             { return m.initErr }
func (m *MockCANDriver) Start()                                  {}
func (m *MockCANDriver) Stop()                                   { m.cancel() }
func (m *MockCANDriver) Context() context.Context                { return m.ctx }
func (m *MockCANDriver) RxChan() <-chan driver.UnifiedCANMessage { return m.rxChan }

func (m *MockCANDriver) Write(id int32, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// 记录写入的数据
	m.writeLog = append(m.writeLog, append([]byte{}, data...))

	// 发送预设响应
	if m.respIndex < len(m.responses) {
		resp := m.responses[m.respIndex]
		m.respIndex++
		go func() {
			time.Sleep(resp.Delay)
			var dataArr [64]byte
			copy(dataArr[:], resp.Data)
			m.rxChan <- driver.UnifiedCANMessage{
				ID:   0x7C7,
				DLC:  byte(len(resp.Data)),
				Data: dataArr,
				IsFD: false,
			}
		}()
	}
	return nil
}

// SetResponses 设置预设响应序列
func (m *MockCANDriver) SetResponses(responses ...MockResponse) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses = responses
	m.respIndex = 0
}

// GetWriteLog 获取写入日志
func (m *MockCANDriver) GetWriteLog() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([][]byte{}, m.writeLog...)
}

// MockTransport 是 isotp.Transport 的简化 Mock
type MockTransport struct {
	mu        sync.Mutex
	sendQueue [][]byte
	recvQueue [][]byte
	fdMode    bool
}

func NewMockTransport() *MockTransport {
	return &MockTransport{}
}

func (t *MockTransport) Send(data []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sendQueue = append(t.sendQueue, append([]byte{}, data...))
}

func (t *MockTransport) Recv() ([]byte, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.recvQueue) == 0 {
		return nil, false
	}
	data := t.recvQueue[0]
	t.recvQueue = t.recvQueue[1:]
	return data, true
}

func (t *MockTransport) SetFDMode(isFD bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.fdMode = isFD
}

func (t *MockTransport) SetTxAddress(addr *isotp.Address) {
	t.mu.Lock()
	defer t.mu.Unlock()
	// Mock implementation: just record that it was called, or store the address if needed checks
}

func (t *MockTransport) Run(ctx context.Context, rxChan <-chan isotp.CanMessage, txChan chan<- isotp.CanMessage) {
	// Mock implementation: do nothing or simulate loop
}

func (t *MockTransport) PushResponse(data []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.recvQueue = append(t.recvQueue, append([]byte{}, data...))
}

// ============================================================================
// 测试用例
// ============================================================================

// TestUDSError_Error 测试 UDSError 的错误消息格式
func TestUDSError_Error(t *testing.T) {
	err := &UDSError{
		ServiceID: 0x22,
		NRC:       NRCServiceNotSupported,
		Message:   "服务不支持",
	}

	expected := "UDS 负响应: SID=0x22, NRC=0x11 (服务不支持)"
	if err.Error() != expected {
		t.Errorf("错误消息不匹配\n期望: %s\n实际: %s", expected, err.Error())
	}
}

// TestUDSError_IsRetryable 测试错误是否可重试
func TestUDSError_IsRetryable(t *testing.T) {
	tests := []struct {
		nrc       byte
		retryable bool
	}{
		{NRCBusyRepeatRequest, true},
		{NRCResponsePending, true},
		{NRCServiceNotSupported, false},
		{NRCSecurityAccessDenied, false},
		{NRCConditionsNotCorrect, false},
	}

	for _, tc := range tests {
		err := &UDSError{NRC: tc.nrc}
		if err.IsRetryable() != tc.retryable {
			t.Errorf("NRC=0x%02X: 期望 IsRetryable()=%v, 实际=%v",
				tc.nrc, tc.retryable, err.IsRetryable())
		}
	}
}

// TestDefaultRequestOptions 测试默认选项
func TestDefaultRequestOptions(t *testing.T) {
	opts := DefaultRequestOptions()

	if opts.Timeout != 500*time.Millisecond {
		t.Errorf("默认超时错误: %v", opts.Timeout)
	}
	if opts.MaxRetries != 3 {
		t.Errorf("默认重试次数错误: %d", opts.MaxRetries)
	}
	if opts.RetryDelay != 100*time.Millisecond {
		t.Errorf("默认重试延迟错误: %v", opts.RetryDelay)
	}
}

// TestGetNRCDescription 测试 NRC 描述获取
func TestGetNRCDescription(t *testing.T) {
	tests := []struct {
		nrc      byte
		expected string
	}{
		{NRCGeneralReject, "一般拒绝"},
		{NRCServiceNotSupported, "服务不支持"},
		{NRCResponsePending, "响应挂起"},
		{0xFF, "未知错误"}, // 未知 NRC
	}

	for _, tc := range tests {
		desc := getNRCDescription(tc.nrc)
		if desc != tc.expected {
			t.Errorf("NRC=0x%02X: 期望描述=%s, 实际=%s", tc.nrc, tc.expected, desc)
		}
	}
}

// TestMockTransport 测试 Mock Transport 基本功能
func TestMockTransport(t *testing.T) {
	transport := NewMockTransport()

	// 测试发送
	transport.Send([]byte{0x22, 0xF1, 0x90})
	if len(transport.sendQueue) != 1 {
		t.Error("发送队列应该有1个元素")
	}

	// 测试接收 (空队列)
	_, ok := transport.Recv()
	if ok {
		t.Error("空队列不应该返回 ok=true")
	}

	// 测试接收 (有数据)
	transport.PushResponse([]byte{0x62, 0xF1, 0x90, 0x01, 0x02})
	data, ok := transport.Recv()
	if !ok || len(data) != 5 {
		t.Error("应该成功接收5字节数据")
	}
}

// TestNRCConstants 测试 NRC 常量值是否正确
func TestNRCConstants(t *testing.T) {
	// 验证一些关键的 NRC 常量值 (根据 ISO 14229 标准)
	tests := []struct {
		name     string
		constant byte
		expected byte
	}{
		{"GeneralReject", NRCGeneralReject, 0x10},
		{"ServiceNotSupported", NRCServiceNotSupported, 0x11},
		{"SubFunctionNotSupported", NRCSubFunctionNotSupported, 0x12},
		{"IncorrectMessageLength", NRCIncorrectMessageLength, 0x13},
		{"BusyRepeatRequest", NRCBusyRepeatRequest, 0x21},
		{"ConditionsNotCorrect", NRCConditionsNotCorrect, 0x22},
		{"RequestOutOfRange", NRCRequestOutOfRange, 0x31},
		{"SecurityAccessDenied", NRCSecurityAccessDenied, 0x33},
		{"ResponsePending", NRCResponsePending, 0x78},
	}

	for _, tc := range tests {
		if tc.constant != tc.expected {
			t.Errorf("%s: 期望=0x%02X, 实际=0x%02X", tc.name, tc.expected, tc.constant)
		}
	}
}

// TestRequestOptions_Validation 测试请求选项
func TestRequestOptions_Validation(t *testing.T) {
	// 测试零值选项
	opts := RequestOptions{}
	if opts.Timeout != 0 {
		t.Error("零值 Timeout 应该为 0")
	}
	if opts.MaxRetries != 0 {
		t.Error("零值 MaxRetries 应该为 0")
	}

	// 测试自定义选项
	opts = RequestOptions{
		Timeout:    1 * time.Second,
		MaxRetries: 5,
		RetryDelay: 200 * time.Millisecond,
	}
	if opts.Timeout != time.Second {
		t.Error("自定义 Timeout 设置错误")
	}
}

// TestUDSError_TypeAssertion 测试错误类型断言
func TestUDSError_TypeAssertion(t *testing.T) {
	var err error = &UDSError{
		ServiceID: 0x10,
		NRC:       NRCConditionsNotCorrect,
		Message:   "条件不满足",
	}

	// 使用 errors.As 进行类型断言
	var udsErr *UDSError
	if !errors.As(err, &udsErr) {
		t.Error("应该能够将 error 断言为 *UDSError")
	}

	if udsErr.ServiceID != 0x10 {
		t.Error("ServiceID 不匹配")
	}
	if udsErr.NRC != NRCConditionsNotCorrect {
		t.Error("NRC 不匹配")
	}
}

// TestSetAddressingMode 测试寻址模式切换
func TestSetAddressingMode(t *testing.T) {
	mockTransport := NewMockTransport()
	// 注意：这里我们需要手动构造 UDSClient，因为 newUDSClient 是未导出的 (或者我们可以导出一个 NewOption?)
	// 但由于我们在同一个包内测试 (package uds_client)，我们可以直接调用 internal 构造函数 newUDSClient
	// 不过 newUDSClient 需要 *driver.ToomossAdapter，这很难 mock。
	// 为了测试方便，我们可能需要重构 newUDSClient 使得它接受 adapter 接口，或者我们手动构造 Client。

	// 手动构造 client 以便注入 mockTransport
	client := &UDSClient{
		stack:    mockTransport,
		mode:     AddressPhysical,
		reqMu:    sync.Mutex{},
		funcAddr: nil,
	}

	// 1. 测试切换到功能寻址 (失败，因为未设置功能地址)
	err := client.SetAddressingMode(AddressFunctional)
	if err == nil {
		t.Error("切换到功能寻址应该失败 (未设置地址)")
	}

	// 2. 设置功能地址
	funcAddr := isotp.Address{TxID: 0x7DF, RxID: 0x7DF}
	err = client.SetFunctionalAddress(&funcAddr)
	if err != nil {
		t.Errorf("设置功能地址失败: %v", err)
	}

	// 3. 测试切换到功能寻址 (成功)
	err = client.SetAddressingMode(AddressFunctional)
	if err != nil {
		t.Errorf("切换到功能寻址失败: %v", err)
	}
	if client.mode != AddressFunctional {
		t.Error("模式应该更新为 AddressFunctional")
	}

	// 4. 测试切换回物理寻址
	err = client.SetAddressingMode(AddressPhysical)
	if err != nil {
		t.Errorf("切换到物理寻址失败: %v", err)
	}
	if client.mode != AddressPhysical {
		t.Error("模式应该更新为 AddressPhysical")
	}
}

// TestMockCANDriver 测试 Mock CAN 驱动
func TestMockCANDriver(t *testing.T) {
	driver := NewMockCANDriver()
	defer driver.Stop()

	// 设置响应
	driver.SetResponses(
		MockResponse{Delay: 10 * time.Millisecond, Data: []byte{0x62, 0xF1, 0x90}},
	)

	// 写入数据
	err := driver.Write(0x747, []byte{0x22, 0xF1, 0x90})
	if err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	// 检查写入日志
	log := driver.GetWriteLog()
	if len(log) != 1 {
		t.Errorf("写入日志应该有1条记录, 实际有 %d 条", len(log))
	}

	// 等待并接收响应
	select {
	case msg := <-driver.RxChan():
		if msg.Data[0] != 0x62 {
			t.Errorf("响应数据错误: %v", msg.Data)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("等待响应超时")
	}
}

// 新增测试：RequestWithContext 在收到负响应时，返回原始响应帧与错误
func TestRequestWithContext_ReturnsResponseOnNRC(t *testing.T) {
	mockTransport := NewMockTransport()
	client := &UDSClient{
		stack: mockTransport,
		mode:  AddressPhysical,
	}

	// 模拟异步到达：推入负响应（小延时，避免被 pre-clear 清掉）
	go func() {
		time.Sleep(1 * time.Millisecond)
		mockTransport.PushResponse([]byte{0x7F, 0x22, NRCServiceNotSupported})
	}()

	resp, err := client.RequestWithContext(context.Background(), []byte{0x22, 0xF1, 0x90}, DefaultRequestOptions())
	if err == nil {
		t.Fatalf("期望返回错误，但 nil")
	}
	if resp == nil {
		t.Fatalf("期望返回原始响应帧，但为 nil")
	}
	if !(len(resp) >= 3 && resp[0] == 0x7F && resp[2] == NRCServiceNotSupported) {
		t.Fatalf("原始响应帧不匹配: %v", resp)
	}
	var udsErr *UDSError
	if !errors.As(err, &udsErr) {
		t.Fatalf("期望 err 为 *UDSError, 实际: %T", err)
	}
	if udsErr.NRC != NRCServiceNotSupported {
		t.Fatalf("UDSError 的 NRC 不匹配")
	}
}

// TestAllNRCDescriptions 测试所有 NRC 都有描述
func TestAllNRCDescriptions(t *testing.T) {
	allNRCs := []byte{
		NRCGeneralReject,
		NRCServiceNotSupported,
		NRCSubFunctionNotSupported,
		NRCIncorrectMessageLength,
		NRCResponseTooLong,
		NRCBusyRepeatRequest,
		NRCConditionsNotCorrect,
		NRCRequestSequenceError,
		NRCNoResponseFromSubnetComponent,
		NRCFailurePreventsExecution,
		NRCRequestOutOfRange,
		NRCSecurityAccessDenied,
		NRCInvalidKey,
		NRCExceedNumberOfAttempts,
		NRCRequiredTimeDelayNotExpired,
		NRCUploadDownloadNotAccepted,
		NRCTransferDataSuspended,
		NRCGeneralProgrammingFailure,
		NRCWrongBlockSequenceCounter,
		NRCResponsePending,
		NRCSubFunctionNotSupportedInActiveSession,
		NRCServiceNotSupportedInActiveSession,
	}

	for _, nrc := range allNRCs {
		desc := getNRCDescription(nrc)
		if desc == "未知错误" {
			t.Errorf("NRC=0x%02X 应该有描述，但返回了 '未知错误'", nrc)
		}
	}
}

// ============================================================================
// Benchmark 测试
// ============================================================================

func BenchmarkGetNRCDescription(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = getNRCDescription(NRCResponsePending)
	}
}

func BenchmarkUDSError_Error(b *testing.B) {
	err := &UDSError{
		ServiceID: 0x22,
		NRC:       NRCServiceNotSupported,
		Message:   "服务不支持",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = err.Error()
	}
}

// 确保 isotp 包被使用（避免编译错误）
var _ = isotp.CanMessage{}
