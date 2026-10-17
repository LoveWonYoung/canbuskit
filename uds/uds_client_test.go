package uds

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/LoveWonYoung/canbuskit/v2/can"
	isotp "github.com/LoveWonYoung/canbuskit/v2/isotp"
)

// ============================================================================
// Mock 实现
// ============================================================================

// MockCANDriver is a test implementation of can.Driver.
type MockCANDriver struct {
	mu        sync.Mutex
	rxChan    chan can.Frame
	cancel    context.CancelFunc
	fdMode    bool
	writeLog  [][]byte       // 记录所有写入的数据
	responses []MockResponse // 预设的响应
	respIndex int
	config    can.Config
}

// MockResponse 定义一个预设的响应
type MockResponse struct {
	Delay time.Duration // 响应延迟
	Data  []byte        // 响应数据
}

func NewMockCANDriver() *MockCANDriver {
	_, cancel := context.WithCancel(context.Background())
	return &MockCANDriver{
		rxChan: make(chan can.Frame, 100),
		cancel: cancel,
		config: can.Config{Mode: can.Classic},
	}
}

func (m *MockCANDriver) Open(context.Context) error { return nil }
func (m *MockCANDriver) Close() error {
	m.cancel()
	return nil
}
func (m *MockCANDriver) Subscribe(int) (<-chan can.Frame, func(), error) {
	return m.rxChan, func() {}, nil
}
func (m *MockCANDriver) Errors() <-chan error {
	ch := make(chan error)
	close(ch)
	return ch
}
func (m *MockCANDriver) Stats() can.Stats     { return can.Stats{} }
func (m *MockCANDriver) BusLoad() can.BusLoad { return can.BusLoad{} }
func (m *MockCANDriver) Config() can.Config {
	cfg := m.config
	if m.fdMode {
		cfg.Mode = can.FD
	}
	return cfg
}

func (m *MockCANDriver) Send(_ context.Context, frame can.Frame) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	data := frame.Payload()
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
			m.rxChan <- can.Frame{
				Direction: can.Receive,
				ID:        0x7C7,
				DLC:       byte(len(resp.Data)),
				Data:      dataArr,
				FD:        false,
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
	recvCh    chan []byte
	fdMode    bool
	txAddr    *isotp.Address
	sendTxIDs []uint32
	autoResp  []byte
	autoDelay time.Duration
	errors    chan error
}

func NewMockTransport() *MockTransport {
	return &MockTransport{
		recvCh: make(chan []byte, 100),
		errors: make(chan error, 10),
	}
}

func (t *MockTransport) SendContext(ctx context.Context, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t.mu.Lock()
	t.sendQueue = append(t.sendQueue, append([]byte{}, data...))
	if t.txAddr != nil {
		t.sendTxIDs = append(t.sendTxIDs, t.txAddr.TxID)
	} else {
		t.sendTxIDs = append(t.sendTxIDs, 0)
	}
	resp := append([]byte{}, t.autoResp...)
	delay := t.autoDelay
	t.mu.Unlock()

	if len(resp) > 0 {
		go func() {
			time.Sleep(delay)
			t.recvCh <- resp
		}()
	}
	return nil
}

func (t *MockTransport) RecvChan() <-chan []byte { return t.recvCh }

func (t *MockTransport) SetTxAddress(addr *isotp.Address) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.txAddr = addr
	return nil
}

func (t *MockTransport) Errors() <-chan error { return t.errors }

func (t *MockTransport) Close() error { return nil }

func (t *MockTransport) PushResponse(data []byte) {
	t.recvCh <- append([]byte{}, data...)
}

func (t *MockTransport) SetAutoResponse(delay time.Duration, data []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.autoDelay = delay
	t.autoResp = append([]byte{}, data...)
}

// ============================================================================
// 测试用例
// ============================================================================

// TestUDSError_Error 测试 UDSError 的错误消息格式
func TestUDSError_Error(t *testing.T) {
	err := &UDSError{
		ServiceID: 0x22,
		NRC:       ServiceNotSupported,
		Message:   "服务不支持",
	}

	expected := "UDS negative response: SID=0x22, NRC=0x11 (服务不支持)"
	if err.Error() != expected {
		t.Errorf("错误消息不匹配\n期望: %s\n实际: %s", expected, err.Error())
	}
}

// TestUDSError_IsRetryable 测试错误是否可重试
func TestUDSError_IsRetryable(t *testing.T) {
	tests := []struct {
		nrc       NRC
		retryable bool
	}{
		{BusyRepeatRequest, true},
		{RequestCorrectlyReceivedResponsePending, true},
		{ServiceNotSupported, false},
		{SecurityAccessDenied, false},
		{ConditionsNotCorrect, false},
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
		nrc      NRC
		expected string
	}{
		{GeneralReject, "GeneralReject"},
		{ServiceNotSupported, "ServiceNotSupported"},
		{RequestCorrectlyReceivedResponsePending, "RequestCorrectlyReceivedResponsePending"},
		{0xFF, "UnknownNRC"}, // 未知 NRC
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
	if err := transport.SendContext(context.Background(), []byte{0x22, 0xF1, 0x90}); err != nil {
		t.Fatal(err)
	}
	if len(transport.sendQueue) != 1 {
		t.Error("发送队列应该有1个元素")
	}

	// 测试接收 (空队列)
	select {
	case <-transport.RecvChan():
		t.Error("空队列不应该返回 ok=true")
	default:
	}

	// 测试接收 (有数据)
	transport.PushResponse([]byte{0x62, 0xF1, 0x90, 0x01, 0x02})
	data := <-transport.RecvChan()
	if len(data) != 5 {
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
		{"GeneralReject", GeneralReject, 0x10},
		{"ServiceNotSupported", ServiceNotSupported, 0x11},
		{"SubFunctionNotSupported", SubFunctionNotSupported, 0x12},
		{"IncorrectMessageLength", IncorrectMessageLengthOrInvalidFormat, 0x13},
		{"BusyRepeatRequest", BusyRepeatRequest, 0x21},
		{"ConditionsNotCorrect", ConditionsNotCorrect, 0x22},
		{"RequestOutOfRange", RequestOutOfRange, 0x31},
		{"SecurityAccessDenied", SecurityAccessDenied, 0x33},
		{"ResponsePending", RequestCorrectlyReceivedResponsePending, 0x78},
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
		NRC:       ConditionsNotCorrect,
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
	if udsErr.NRC != ConditionsNotCorrect {
		t.Error("NRC 不匹配")
	}
}

func TestRequestWithContextAndAddressingModeUsesFunctionalAddressForSingleRequest(t *testing.T) {
	mockTransport := NewMockTransport()
	mockTransport.SetAutoResponse(1*time.Millisecond, []byte{0x62, 0xF1, 0x90})
	funcAddr := isotp.Address{TxID: 0x7DF, RxID: 0x7E8}
	client := &Client{
		stack:    mockTransport,
		funcAddr: &funcAddr,
	}

	resp, err := client.RequestWithContextAndAddressingMode(
		context.Background(),
		[]byte{0x22, 0xF1, 0x90},
		RequestOptions{Timeout: 50 * time.Millisecond},
		AddressFunctional,
	)
	if err != nil {
		t.Fatalf("功能寻址请求失败: %v", err)
	}
	if len(resp) == 0 || resp[0] != 0x62 {
		t.Fatalf("响应不匹配: %v", resp)
	}
	if len(mockTransport.sendTxIDs) != 1 || mockTransport.sendTxIDs[0] != 0x7DF {
		t.Fatalf("功能寻址发送 ID 不匹配: %v", mockTransport.sendTxIDs)
	}

	mockTransport.SetAutoResponse(1*time.Millisecond, []byte{0x62, 0xF1, 0x91})
	_, err = client.RequestWithContext(
		context.Background(),
		[]byte{0x22, 0xF1, 0x91},
		RequestOptions{Timeout: 50 * time.Millisecond},
	)
	if err != nil {
		t.Fatalf("后续物理寻址请求失败: %v", err)
	}
	if got := mockTransport.sendTxIDs[len(mockTransport.sendTxIDs)-1]; got != 0 {
		t.Fatalf("后续默认请求应该恢复物理寻址，实际 tx id: 0x%X", got)
	}
}

// TestMockCANDriver 测试 Mock CAN 驱动
func TestMockCANDriver(t *testing.T) {
	driver := NewMockCANDriver()
	defer driver.Close()

	// 设置响应
	driver.SetResponses(
		MockResponse{Delay: 10 * time.Millisecond, Data: []byte{0x62, 0xF1, 0x90}},
	)

	// 写入数据
	frame, err := can.NewFrame(0x747, []byte{0x22, 0xF1, 0x90}, can.FrameFlags{})
	if err != nil {
		t.Fatal(err)
	}
	err = driver.Send(context.Background(), frame)
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
	case msg := <-driver.rxChan:
		if msg.Data[0] != 0x62 {
			t.Errorf("响应数据错误: %v", msg.Data)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("等待响应超时")
	}
}

func TestNewUDSClient_AutoSyncFDMode(t *testing.T) {
	mockDriver := NewMockCANDriver()
	mockDriver.fdMode = true

	addr, err := isotp.NewAddress(0x700, 0x708)
	if err != nil {
		t.Fatalf("create address failed: %v", err)
	}

	client, err := New(mockDriver, addr, isotp.DefaultConfig())
	if err != nil {
		t.Fatalf("new uds client failed: %v", err)
	}
	defer client.Close()

	stack, ok := client.stack.(*isotp.Session)
	if !ok {
		t.Fatalf("unexpected stack type: %T", client.stack)
	}
	if !stack.FDMode() {
		t.Fatalf("expected stack fd mode to be true")
	}
	if stack.MaxFrameDataLength() != 64 {
		t.Fatalf("expected max data length 64, got %d", stack.MaxFrameDataLength())
	}
}

func TestUDSClientContinuesAfterFilteredTXEcho(t *testing.T) {
	mockDriver := NewMockCANDriver()
	mockDriver.rxChan <- can.Frame{
		Direction: can.Transmit,
		ID:        0x700,
		DLC:       1,
	}
	mockDriver.SetResponses(MockResponse{
		Data: []byte{0x04, 0x62, 0xF1, 0x90, 0x01},
	})

	addr, err := isotp.NewAddress(0x700, 0x7C7)
	if err != nil {
		t.Fatal(err)
	}
	client, err := New(mockDriver, addr, isotp.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	resp, err := client.RequestWithTimeout([]byte{0x22, 0xF1, 0x90}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x62, 0xF1, 0x90, 0x01}
	if len(resp) != len(want) {
		t.Fatalf("response length = %d, want %d", len(resp), len(want))
	}
	for i := range want {
		if resp[i] != want[i] {
			t.Fatalf("response = % X, want % X", resp, want)
		}
	}
}

// 新增测试：RequestWithContext 在收到负响应时，返回原始响应帧与错误
func TestRequestWithContext_ReturnsResponseOnNRC(t *testing.T) {
	mockTransport := NewMockTransport()
	client := &Client{
		stack: mockTransport,
	}

	// 模拟异步到达：推入负响应（小延时，避免被 pre-clear 清掉）
	go func() {
		time.Sleep(1 * time.Millisecond)
		mockTransport.PushResponse([]byte{0x7F, 0x22, ServiceNotSupported})
	}()

	resp, err := client.RequestWithContext(context.Background(), []byte{0x22, 0xF1, 0x90}, DefaultRequestOptions())
	if err == nil {
		t.Fatalf("期望返回错误，但 nil")
	}
	if resp == nil {
		t.Fatalf("期望返回原始响应帧，但为 nil")
	}
	if !(len(resp) >= 3 && resp[0] == 0x7F && resp[2] == ServiceNotSupported) {
		t.Fatalf("原始响应帧不匹配: %v", resp)
	}
	var udsErr *UDSError
	if !errors.As(err, &udsErr) {
		t.Fatalf("期望 err 为 *UDSError, 实际: %T", err)
	}
	if udsErr.NRC != ServiceNotSupported {
		t.Fatalf("UDSError 的 NRC 不匹配")
	}
}

func TestNewDoesNotOwnDriverLifecycle(t *testing.T) {
	dev := NewMockCANDriver()
	addr, err := isotp.NewAddress(0x7C6, 0x7C7)
	if err != nil {
		t.Fatal(err)
	}
	client, err := New(dev, addr, isotp.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	client.Close()
	if dev.cancel == nil {
		t.Fatal("test driver was unexpectedly modified")
	}
}

func TestRequestIgnoresResponsesForAnotherService(t *testing.T) {
	transport := NewMockTransport()
	client := &Client{stack: transport}
	go func() {
		time.Sleep(time.Millisecond)
		transport.PushResponse([]byte{0x7F, 0x10, ServiceNotSupported})
		transport.PushResponse([]byte{0x50, 0x03})
		transport.PushResponse([]byte{0x62, 0xF1, 0x90})
	}()

	resp, err := client.RequestWithContext(context.Background(), []byte{0x22, 0xF1, 0x90}, RequestOptions{Timeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp) == 0 || resp[0] != 0x62 {
		t.Fatalf("response = % X, want matching 0x62 response", resp)
	}
}

func TestResponsePendingLimitReturnsLastFrame(t *testing.T) {
	transport := NewMockTransport()
	client := &Client{stack: transport}
	go func() {
		time.Sleep(time.Millisecond)
		transport.PushResponse([]byte{0x7F, 0x22, RequestCorrectlyReceivedResponsePending})
		transport.PushResponse([]byte{0x7F, 0x22, RequestCorrectlyReceivedResponsePending})
	}()
	opts := RequestOptions{
		Timeout:                100 * time.Millisecond,
		ResponsePendingTimeout: 100 * time.Millisecond,
		MaxResponsePending:     1,
	}
	resp, err := client.RequestWithContext(context.Background(), []byte{0x22}, opts)
	if err == nil {
		t.Fatal("expected response-pending limit error")
	}
	if len(resp) < 3 || resp[2] != RequestCorrectlyReceivedResponsePending {
		t.Fatalf("response = % X, want final response-pending frame", resp)
	}
}

func TestRetryDelayHonorsContextCancellation(t *testing.T) {
	transport := NewMockTransport()
	client := &Client{stack: transport}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(time.Millisecond)
		transport.PushResponse([]byte{0x7F, 0x22, BusyRepeatRequest})
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	opts := RequestOptions{Timeout: 100 * time.Millisecond, MaxRetries: 1, RetryDelay: time.Second}
	started := time.Now()
	_, err := client.RequestWithContext(ctx, []byte{0x22}, opts)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RequestWithContext() error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("context cancellation was delayed by retry sleep: %s", elapsed)
	}
}

func TestCloseIsIdempotentAndClosedRequestReturns(t *testing.T) {
	client := newClient(NewMockTransport())
	client.Close()
	client.Close()

	done := make(chan error, 1)
	go func() {
		_, err := client.RequestWithContext(context.Background(), []byte{0x22}, RequestOptions{Timeout: time.Second})
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrClientClosed) {
			t.Fatalf("closed request error = %v, want ErrClientClosed", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("request blocked after client close")
	}
}

func TestErrorsForwardsTransportErrors(t *testing.T) {
	transport := NewMockTransport()
	client := newClient(transport)
	defer client.Close()
	want := errors.New("transport failure")
	transport.errors <- want
	select {
	case got := <-client.Errors():
		if !errors.Is(got, want) {
			t.Fatalf("Errors() returned %v, want %v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("transport error was not forwarded")
	}
}

// TestAllNRCDescriptions 测试所有 NRC 都有描述
func TestAllNRCDescriptions(t *testing.T) {
	allNRCs := []NRC{
		GeneralReject,
		ServiceNotSupported,
		SubFunctionNotSupported,
		IncorrectMessageLengthOrInvalidFormat,
		ResponseTooLong,
		BusyRepeatRequest,
		ConditionsNotCorrect,
		RequestSequenceError,
		NoResponseFromSubnetComponent,
		FailurePreventsExecutionOfRequestedAction,
		RequestOutOfRange,
		SecurityAccessDenied,
		InvalidKey,
		ExceedNumberOfAttempts,
		RequiredTimeDelayNotExpired,
		UploadDownloadNotAccepted,
		TransferDataSuspended,
		GeneralProgrammingFailure,
		WrongBlockSequenceCounter,
		RequestCorrectlyReceivedResponsePending,
		SubFunctionNotSupportedInActiveSession,
		ServiceNotSupportedInActiveSession,
	}

	for _, nrc := range allNRCs {
		desc := getNRCDescription(nrc)
		if desc == "UnknownNRC" {
			t.Errorf("NRC=0x%02X should have a description", nrc)
		}
	}
}

// ============================================================================
// Benchmark 测试
// ============================================================================

func BenchmarkGetNRCDescription(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = getNRCDescription(RequestCorrectlyReceivedResponsePending)
	}
}

func BenchmarkUDSError_Error(b *testing.B) {
	err := &UDSError{
		ServiceID: 0x22,
		NRC:       ServiceNotSupported,
		Message:   "服务不支持",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = err.Error()
	}
}

// 确保 isotp 包被使用（避免编译错误）
