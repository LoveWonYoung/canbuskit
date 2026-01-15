package tp_layer

import (
	"bytes"
	"testing"
	"time"
)

// ============================================================================
// 常量定义
// ============================================================================

const (
	CANMaxDataLength   = 8  // CAN 最大数据长度
	CANFDMaxDataLength = 64 // CAN-FD 最大数据长度
)

// ============================================================================
// 单帧 (Single Frame) 测试
// ============================================================================

// TestCreateSingleFrame_CAN 测试 CAN 单帧创建
func TestCreateSingleFrame_CAN(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected []byte
	}{
		{
			name:     "1字节数据",
			data:     []byte{0x22},
			expected: []byte{0x01, 0x22},
		},
		{
			name:     "3字节数据 (典型UDS请求)",
			data:     []byte{0x22, 0xF1, 0x90},
			expected: []byte{0x03, 0x22, 0xF1, 0x90},
		},
		{
			name:     "7字节数据 (CAN最大单帧)",
			data:     []byte{0x22, 0xF1, 0x90, 0x01, 0x02, 0x03, 0x04},
			expected: []byte{0x07, 0x22, 0xF1, 0x90, 0x01, 0x02, 0x03, 0x04},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := createSingleFramePayload(tc.data, CANMaxDataLength)
			if err != nil {
				t.Fatalf("创建单帧失败: %v", err)
			}
			if !bytes.Equal(result, tc.expected) {
				t.Errorf("单帧数据不匹配\n期望: % 02X\n实际: % 02X", tc.expected, result)
			}
		})
	}
}

// TestCreateSingleFrame_CANFD 测试 CAN-FD 单帧创建 (包含转义长度)
func TestCreateSingleFrame_CANFD(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected []byte
	}{
		{
			name:     "7字节数据 (标准格式)",
			data:     []byte{0x22, 0xF1, 0x90, 0x01, 0x02, 0x03, 0x04},
			expected: []byte{0x07, 0x22, 0xF1, 0x90, 0x01, 0x02, 0x03, 0x04},
		},
		{
			name:     "10字节数据 (FD转义格式)",
			data:     []byte{0x62, 0xF1, 0x90, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07},
			expected: []byte{0x00, 0x0A, 0x62, 0xF1, 0x90, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07},
		},
		{
			name:     "20字节数据",
			data:     bytes.Repeat([]byte{0xAA}, 20),
			expected: append([]byte{0x00, 0x14}, bytes.Repeat([]byte{0xAA}, 20)...),
		},
		{
			name:     "62字节数据 (CAN-FD最大单帧)",
			data:     bytes.Repeat([]byte{0xBB}, 62),
			expected: append([]byte{0x00, 0x3E}, bytes.Repeat([]byte{0xBB}, 62)...),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := createSingleFramePayload(tc.data, CANFDMaxDataLength)
			if err != nil {
				t.Fatalf("创建CAN-FD单帧失败: %v", err)
			}
			if !bytes.Equal(result, tc.expected) {
				t.Errorf("CAN-FD单帧数据不匹配\n期望: % 02X\n实际: % 02X", tc.expected, result)
			}
		})
	}
}

// TestCreateSingleFrame_Overflow 测试单帧溢出
func TestCreateSingleFrame_Overflow(t *testing.T) {
	// CAN 模式下尝试发送超过7字节的数据应该失败
	_, err := createSingleFramePayload(bytes.Repeat([]byte{0xFF}, 8), CANMaxDataLength)
	if err == nil {
		t.Error("应该返回溢出错误")
	}

	// CAN-FD 模式下尝试发送超过62字节的数据应该失败
	_, err = createSingleFramePayload(bytes.Repeat([]byte{0xFF}, 63), CANFDMaxDataLength)
	if err == nil {
		t.Error("CAN-FD单帧应该返回溢出错误")
	}
}

// ============================================================================
// 首帧 (First Frame) 测试
// ============================================================================

// TestCreateFirstFrame_CAN 测试 CAN 首帧创建
func TestCreateFirstFrame_CAN(t *testing.T) {
	tests := []struct {
		name           string
		firstChunk     []byte
		totalSize      int
		expectedPrefix []byte
	}{
		{
			name:           "100字节消息的首帧",
			firstChunk:     []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
			totalSize:      100,
			expectedPrefix: []byte{0x10, 0x64}, // 0x10 | (100>>8), 100&0xFF
		},
		{
			name:           "4095字节消息的首帧 (12位最大值)",
			firstChunk:     []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
			totalSize:      4095,
			expectedPrefix: []byte{0x1F, 0xFF}, // 0x10 | 0x0F, 0xFF
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := createFirstFramePayload(tc.firstChunk, tc.totalSize, CANMaxDataLength)
			if err != nil {
				t.Fatalf("创建首帧失败: %v", err)
			}
			// 检查PCI前缀
			if !bytes.Equal(result[:2], tc.expectedPrefix) {
				t.Errorf("首帧PCI不匹配\n期望: % 02X\n实际: % 02X", tc.expectedPrefix, result[:2])
			}
			// 检查数据部分
			if !bytes.Equal(result[2:], tc.firstChunk) {
				t.Errorf("首帧数据不匹配\n期望: % 02X\n实际: % 02X", tc.firstChunk, result[2:])
			}
		})
	}
}

// TestCreateFirstFrame_CANFD_LongMessage 测试 CAN-FD 超长消息首帧 (32位长度)
func TestCreateFirstFrame_CANFD_LongMessage(t *testing.T) {
	// 超过4095字节的消息需要使用32位长度格式
	firstChunk := bytes.Repeat([]byte{0xCC}, 58) // 64 - 6 = 58 字节数据
	totalSize := 10000                           // 超过4095

	result, err := createFirstFramePayload(firstChunk, totalSize, CANFDMaxDataLength)
	if err != nil {
		t.Fatalf("创建长消息首帧失败: %v", err)
	}

	// 检查PCI (6字节): 0x10, 0x00, 然后是4字节大端长度
	expectedPCI := []byte{0x10, 0x00, 0x00, 0x00, 0x27, 0x10} // 10000 = 0x2710
	if !bytes.Equal(result[:6], expectedPCI) {
		t.Errorf("长消息首帧PCI不匹配\n期望: % 02X\n实际: % 02X", expectedPCI, result[:6])
	}
}

// ============================================================================
// 连续帧 (Consecutive Frame) 测试
// ============================================================================

// TestCreateConsecutiveFrame 测试连续帧创建
func TestCreateConsecutiveFrame(t *testing.T) {
	tests := []struct {
		seqNum   int
		data     []byte
		expected []byte
	}{
		{
			seqNum:   1,
			data:     []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07},
			expected: []byte{0x21, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07},
		},
		{
			seqNum:   15,
			data:     []byte{0xAA, 0xBB, 0xCC},
			expected: []byte{0x2F, 0xAA, 0xBB, 0xCC},
		},
		{
			seqNum:   0, // 序列号从1开始，但0也是有效的(环绕后)
			data:     []byte{0x11, 0x22},
			expected: []byte{0x20, 0x11, 0x22},
		},
	}

	for _, tc := range tests {
		t.Run("SeqNum_"+string(rune('0'+tc.seqNum)), func(t *testing.T) {
			result, err := createConsecutiveFramePayload(tc.data, tc.seqNum)
			if err != nil {
				t.Fatalf("创建连续帧失败: %v", err)
			}
			if !bytes.Equal(result, tc.expected) {
				t.Errorf("连续帧数据不匹配\n期望: % 02X\n实际: % 02X", tc.expected, result)
			}
		})
	}
}

// TestCreateConsecutiveFrame_InvalidSeqNum 测试无效序列号
func TestCreateConsecutiveFrame_InvalidSeqNum(t *testing.T) {
	_, err := createConsecutiveFramePayload([]byte{0x01}, 16)
	if err == nil {
		t.Error("序列号16应该返回错误")
	}

	_, err = createConsecutiveFramePayload([]byte{0x01}, -1)
	if err == nil {
		t.Error("序列号-1应该返回错误")
	}
}

// ============================================================================
// 流控帧 (Flow Control Frame) 测试
// ============================================================================

// TestCreateFlowControlFrame 测试流控帧创建
func TestCreateFlowControlFrame(t *testing.T) {
	tests := []struct {
		name      string
		status    FlowStatus
		blockSize int
		stMinMs   int
		expected  []byte
	}{
		{
			name:      "CTS (继续发送)",
			status:    FlowStatusContinueToSend,
			blockSize: 0,
			stMinMs:   20,
			expected:  []byte{0x30, 0x00, 0x14},
		},
		{
			name:      "Wait (等待)",
			status:    FlowStatusWait,
			blockSize: 8,
			stMinMs:   50,
			expected:  []byte{0x31, 0x08, 0x32},
		},
		{
			name:      "Overflow (溢出)",
			status:    FlowStatusOverflow,
			blockSize: 0,
			stMinMs:   0,
			expected:  []byte{0x32, 0x00, 0x00},
		},
		{
			name:      "STmin最大值 (127ms)",
			status:    FlowStatusContinueToSend,
			blockSize: 15,
			stMinMs:   127,
			expected:  []byte{0x30, 0x0F, 0x7F},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := createFlowControlPayload(tc.status, tc.blockSize, tc.stMinMs)
			if !bytes.Equal(result, tc.expected) {
				t.Errorf("流控帧数据不匹配\n期望: % 02X\n实际: % 02X", tc.expected, result)
			}
		})
	}
}

// ============================================================================
// 帧解析 (ParseFrame) 测试
// ============================================================================

// TestParseFrame_SingleFrame 测试单帧解析
func TestParseFrame_SingleFrame(t *testing.T) {
	tests := []struct {
		name         string
		data         []byte
		expectedLen  int
		expectedData []byte
	}{
		{
			name:         "CAN单帧",
			data:         []byte{0x03, 0x22, 0xF1, 0x90, 0x00, 0x00, 0x00, 0x00},
			expectedLen:  3,
			expectedData: []byte{0x22, 0xF1, 0x90},
		},
		{
			name:         "CAN-FD单帧 (转义长度)",
			data:         []byte{0x00, 0x0A, 0x62, 0xF1, 0x90, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07},
			expectedLen:  10,
			expectedData: []byte{0x62, 0xF1, 0x90, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := &CanMessage{ArbitrationID: 0x7C7, Data: tc.data}
			frame, err := ParseFrame(msg, 0)
			if err != nil {
				t.Fatalf("解析单帧失败: %v", err)
			}
			sf, ok := frame.(*SingleFrame)
			if !ok {
				t.Fatal("应该解析为 SingleFrame")
			}
			if len(sf.Data) != tc.expectedLen {
				t.Errorf("数据长度不匹配: 期望=%d, 实际=%d", tc.expectedLen, len(sf.Data))
			}
			if !bytes.Equal(sf.Data, tc.expectedData) {
				t.Errorf("数据不匹配\n期望: % 02X\n实际: % 02X", tc.expectedData, sf.Data)
			}
		})
	}
}

// TestParseFrame_FirstFrame 测试首帧解析
func TestParseFrame_FirstFrame(t *testing.T) {
	tests := []struct {
		name         string
		data         []byte
		expectedSize int
		expectedData []byte
	}{
		{
			name:         "12位长度首帧",
			data:         []byte{0x10, 0x64, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
			expectedSize: 100,
			expectedData: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
		},
		{
			name:         "32位长度首帧",
			data:         []byte{0x10, 0x00, 0x00, 0x00, 0x27, 0x10, 0xAA, 0xBB},
			expectedSize: 10000,
			expectedData: []byte{0xAA, 0xBB},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := &CanMessage{ArbitrationID: 0x7C7, Data: tc.data}
			frame, err := ParseFrame(msg, 0)
			if err != nil {
				t.Fatalf("解析首帧失败: %v", err)
			}
			ff, ok := frame.(*FirstFrame)
			if !ok {
				t.Fatal("应该解析为 FirstFrame")
			}
			if ff.TotalSize != tc.expectedSize {
				t.Errorf("总大小不匹配: 期望=%d, 实际=%d", tc.expectedSize, ff.TotalSize)
			}
			if !bytes.Equal(ff.Data, tc.expectedData) {
				t.Errorf("数据不匹配\n期望: % 02X\n实际: % 02X", tc.expectedData, ff.Data)
			}
		})
	}
}

// TestParseFrame_ConsecutiveFrame 测试连续帧解析
func TestParseFrame_ConsecutiveFrame(t *testing.T) {
	data := []byte{0x21, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}
	msg := &CanMessage{ArbitrationID: 0x7C7, Data: data}

	frame, err := ParseFrame(msg, 0)
	if err != nil {
		t.Fatalf("解析连续帧失败: %v", err)
	}

	cf, ok := frame.(*ConsecutiveFrame)
	if !ok {
		t.Fatal("应该解析为 ConsecutiveFrame")
	}

	if cf.SequenceNumber != 1 {
		t.Errorf("序列号不匹配: 期望=1, 实际=%d", cf.SequenceNumber)
	}

	expectedData := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}
	if !bytes.Equal(cf.Data, expectedData) {
		t.Errorf("数据不匹配\n期望: % 02X\n实际: % 02X", expectedData, cf.Data)
	}
}

// TestParseFrame_FlowControl 测试流控帧解析
func TestParseFrame_FlowControl(t *testing.T) {
	tests := []struct {
		name           string
		data           []byte
		expectedStatus FlowStatus
		expectedBS     int
		expectedSTmin  time.Duration
	}{
		{
			name:           "CTS帧",
			data:           []byte{0x30, 0x08, 0x14, 0x00, 0x00, 0x00, 0x00, 0x00},
			expectedStatus: FlowStatusContinueToSend,
			expectedBS:     8,
			expectedSTmin:  20 * time.Millisecond,
		},
		{
			name:           "Wait帧",
			data:           []byte{0x31, 0x00, 0x32, 0x00, 0x00, 0x00, 0x00, 0x00},
			expectedStatus: FlowStatusWait,
			expectedBS:     0,
			expectedSTmin:  50 * time.Millisecond,
		},
		{
			name:           "STmin微秒编码 (100us)",
			data:           []byte{0x30, 0x00, 0xF1, 0x00, 0x00, 0x00, 0x00, 0x00},
			expectedStatus: FlowStatusContinueToSend,
			expectedBS:     0,
			expectedSTmin:  100 * time.Microsecond,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := &CanMessage{ArbitrationID: 0x747, Data: tc.data}
			frame, err := ParseFrame(msg, 0)
			if err != nil {
				t.Fatalf("解析流控帧失败: %v", err)
			}
			fc, ok := frame.(*FlowControlFrame)
			if !ok {
				t.Fatal("应该解析为 FlowControlFrame")
			}
			if fc.FlowStatus != tc.expectedStatus {
				t.Errorf("FlowStatus不匹配: 期望=%d, 实际=%d", tc.expectedStatus, fc.FlowStatus)
			}
			if fc.BlockSize != tc.expectedBS {
				t.Errorf("BlockSize不匹配: 期望=%d, 实际=%d", tc.expectedBS, fc.BlockSize)
			}
			if fc.STmin != tc.expectedSTmin {
				t.Errorf("STmin不匹配: 期望=%v, 实际=%v", tc.expectedSTmin, fc.STmin)
			}
		})
	}
}

// TestParseFrame_WithPrefix 测试带前缀的帧解析 (扩展地址模式)
func TestParseFrame_WithPrefix(t *testing.T) {
	// 带1字节地址前缀的单帧
	data := []byte{0x55, 0x03, 0x22, 0xF1, 0x90, 0x00, 0x00, 0x00} // 0x55是地址前缀
	msg := &CanMessage{ArbitrationID: 0x7C7, Data: data}

	frame, err := ParseFrame(msg, 1) // 前缀长度为1
	if err != nil {
		t.Fatalf("解析带前缀的帧失败: %v", err)
	}

	sf, ok := frame.(*SingleFrame)
	if !ok {
		t.Fatal("应该解析为 SingleFrame")
	}

	expectedData := []byte{0x22, 0xF1, 0x90}
	if !bytes.Equal(sf.Data, expectedData) {
		t.Errorf("数据不匹配\n期望: % 02X\n实际: % 02X", expectedData, sf.Data)
	}
}

// ============================================================================
// 完整分帧场景测试
// ============================================================================

// TestCompleteFraming_CAN_MultiFrame 测试 CAN 多帧传输分帧
func TestCompleteFraming_CAN_MultiFrame(t *testing.T) {
	// 模拟发送100字节数据
	totalData := bytes.Repeat([]byte{0xAA}, 100)

	// 首帧: 2字节PCI + 6字节数据
	ffData := totalData[:6]
	ff, err := createFirstFramePayload(ffData, 100, CANMaxDataLength)
	if err != nil {
		t.Fatalf("创建首帧失败: %v", err)
	}
	if len(ff) != 8 {
		t.Errorf("CAN首帧长度应为8字节: %d", len(ff))
	}

	// 连续帧: 1字节PCI + 7字节数据
	remaining := totalData[6:]
	seqNum := 1
	cfCount := 0
	for len(remaining) > 0 {
		chunkSize := 7
		if len(remaining) < chunkSize {
			chunkSize = len(remaining)
		}
		cf, err := createConsecutiveFramePayload(remaining[:chunkSize], seqNum)
		if err != nil {
			t.Fatalf("创建连续帧%d失败: %v", seqNum, err)
		}
		if len(cf) > 8 {
			t.Errorf("CAN连续帧长度不应超过8字节: %d", len(cf))
		}
		remaining = remaining[chunkSize:]
		seqNum = (seqNum + 1) % 16
		cfCount++
	}

	// 验证连续帧数量: (100-6) / 7 = 13.4 -> 14帧
	expectedCFCount := (100 - 6 + 6) / 7 // 向上取整
	if cfCount != expectedCFCount {
		t.Errorf("连续帧数量不匹配: 期望=%d, 实际=%d", expectedCFCount, cfCount)
	}
}

// TestCompleteFraming_CANFD_MultiFrame 测试 CAN-FD 多帧传输分帧
func TestCompleteFraming_CANFD_MultiFrame(t *testing.T) {
	// 模拟发送5000字节数据 (使用32位长度)
	totalData := bytes.Repeat([]byte{0xBB}, 5000)

	// 首帧: 6字节PCI + 58字节数据
	ffData := totalData[:58]
	ff, err := createFirstFramePayload(ffData, 5000, CANFDMaxDataLength)
	if err != nil {
		t.Fatalf("创建CAN-FD首帧失败: %v", err)
	}
	if len(ff) != 64 {
		t.Errorf("CAN-FD首帧长度应为64字节: %d", len(ff))
	}
	// 验证使用32位长度格式
	if ff[0] != 0x10 || ff[1] != 0x00 {
		t.Error("5000字节消息应使用32位长度格式")
	}

	// 连续帧: 1字节PCI + 63字节数据
	remaining := totalData[58:]
	seqNum := 1
	cfCount := 0
	for len(remaining) > 0 {
		chunkSize := 63
		if len(remaining) < chunkSize {
			chunkSize = len(remaining)
		}
		cf, err := createConsecutiveFramePayload(remaining[:chunkSize], seqNum)
		if err != nil {
			t.Fatalf("创建CAN-FD连续帧%d失败: %v", seqNum, err)
		}
		if len(cf) > 64 {
			t.Errorf("CAN-FD连续帧长度不应超过64字节: %d", len(cf))
		}
		remaining = remaining[chunkSize:]
		seqNum = (seqNum + 1) % 16
		cfCount++
	}

	t.Logf("CAN-FD 5000字节消息: 1个首帧 + %d个连续帧", cfCount)
}

// ============================================================================
// STmin 解码测试
// ============================================================================

// TestDecodeSTmin 测试 STmin 解码
func TestDecodeSTmin(t *testing.T) {
	tests := []struct {
		input    byte
		expected time.Duration
	}{
		{0x00, 0},
		{0x14, 20 * time.Millisecond},
		{0x7F, 127 * time.Millisecond},
		{0xF1, 100 * time.Microsecond},
		{0xF5, 500 * time.Microsecond},
		{0xF9, 900 * time.Microsecond},
		{0x80, 127 * time.Millisecond}, // 保留值，使用最大值
		{0xF0, 127 * time.Millisecond}, // 保留值
	}

	for _, tc := range tests {
		result := decodeSTmin(tc.input)
		if result != tc.expected {
			t.Errorf("STmin(0x%02X): 期望=%v, 实际=%v", tc.input, tc.expected, result)
		}
	}
}

// ============================================================================
// 基准测试
// ============================================================================

func BenchmarkCreateSingleFrame_CAN(b *testing.B) {
	data := []byte{0x22, 0xF1, 0x90}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = createSingleFramePayload(data, CANMaxDataLength)
	}
}

func BenchmarkCreateSingleFrame_CANFD(b *testing.B) {
	data := bytes.Repeat([]byte{0xAA}, 20)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = createSingleFramePayload(data, CANFDMaxDataLength)
	}
}

func BenchmarkParseFrame_SingleFrame(b *testing.B) {
	msg := &CanMessage{
		ArbitrationID: 0x7C7,
		Data:          []byte{0x03, 0x22, 0xF1, 0x90, 0x00, 0x00, 0x00, 0x00},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParseFrame(msg, 0)
	}
}

func BenchmarkCreateConsecutiveFrame(b *testing.B) {
	data := bytes.Repeat([]byte{0xBB}, 7)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = createConsecutiveFramePayload(data, i%16)
	}
}
