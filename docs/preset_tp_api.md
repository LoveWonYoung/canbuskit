# Preset 可选 UDS 与手动 ISO-TP API

本文档说明 `preset` 和 `tp_layer` 新增的公开接口，包括：

- 默认关闭 UDS client
- 显式启用 UDS client
- 不使用 UDS client 时手动构造、发送 ISO-TP 帧
- 手动 TP 帧的 8 字节填充开关

## Preset 默认行为

所有 `NewPreset*` 构造函数现在都接受可选的 `preset.Option`：

```go
func NewPresetToomoss(..., options ...preset.Option) (*preset.Preset, error)
func NewPresetAuto(..., options ...preset.Option) (*preset.Preset, error)
func NewPresetTSMaster(..., options ...preset.Option) (*preset.Preset, error)
func NewPresetPCAN(..., options ...preset.Option) (*preset.Preset, error)
func NewPresetVector(..., options ...preset.Option) (*preset.Preset, error)
```

未传入选项时，Preset 只初始化并启动 CAN 设备：

```go
p, err := preset.NewPresetToomoss(
	0x7C6, 0x7C7, 0x7DF,
	driver.CHANNEL1, driver.CANFD,
)
if err != nil {
	return err
}
defer p.Close()
```

此时：

- `p.Client == nil`
- 不注册 UDS 接收订阅
- 可以使用 `Write`、`Read` 和手动 TP 接口
- 调用 `Request` 或 `FunctionRequest` 会返回 `preset.ErrUDSClientDisabled`

## 启用 UDS client

### `WithUDSClient`

```go
func WithUDSClient(enabled bool) preset.Option
```

只有显式启用时，Preset 才创建并注册 UDS client：

```go
p, err := preset.NewPresetToomoss(
	0x7C6, 0x7C7, 0x7DF,
	driver.CHANNEL1, driver.CANFD,
	preset.WithUDSClient(true),
)
if err != nil {
	return err
}
defer p.Close()

resp, err := p.Request([]byte{0x22, 0xF1, 0x90}, time.Second)
```

未启用 UDS 时，以下方法返回 `preset.ErrUDSClientDisabled`：

- `Request`
- `FunctionRequest`
- `SetDefaultStMin`
- `SetDefaultBlockSize`

`SetManualFlowControl` 在未启用 UDS 时为空操作。

## 手动构造 ISO-TP 帧

`tp_layer` 提供四个无状态编码函数。它们不会启动 TP 状态机、不会注册 RX
订阅，也不会自动填充帧。

### `CreateSingleFrame`

```go
func CreateSingleFrame(data []byte, isFD bool) ([]byte, error)
```

构造单帧。Classic CAN 最多携带 7 字节应用数据；CAN FD 最多携带 62 字节。

```go
frame, err := tp_layer.CreateSingleFrame(
	[]byte{0x22, 0xF1, 0x90},
	false,
)
// frame: 03 22 F1 90
```

### `CreateFirstFrame`

```go
func CreateFirstFrame(
	firstChunk []byte,
	totalMessageSize int,
	isFD bool,
) ([]byte, error)
```

`firstChunk` 是首帧本次携带的数据，`totalMessageSize` 是完整 ISO-TP 消息的总长度。

```go
frame, err := tp_layer.CreateFirstFrame(
	[]byte{1, 2, 3, 4, 5, 6},
	20,
	false,
)
// frame: 10 14 01 02 03 04 05 06
```

总长度大于 `4095` 时自动使用 ISO-TP 32 位长度格式。

### `CreateFlowControlFrame`

```go
func CreateFlowControlFrame(
	status tp_layer.FlowStatus,
	blockSize int,
	stMin byte,
) ([]byte, error)
```

可用的流控状态：

| 常量 | 值 | 含义 |
| --- | ---: | --- |
| `FlowStatusContinueToSend` | `0x00` | CTS，允许继续发送 |
| `FlowStatusWait` | `0x01` | WAIT，要求等待 |
| `FlowStatusOverflow` | `0x02` | Overflow，接收方溢出 |

`blockSize` 有效范围为 `0–255`。`stMin` 使用 ISO-TP 原始字节编码：

- `0x00–0x7F`：0–127 ms
- `0xF1–0xF9`：100–900 μs

```go
frame, err := tp_layer.CreateFlowControlFrame(
	tp_layer.FlowStatusContinueToSend,
	8,
	0x05,
)
// frame: 30 08 05
```

### `CreateConsecutiveFrame`

```go
func CreateConsecutiveFrame(
	dataChunk []byte,
	sequenceNumber int,
	isFD bool,
) ([]byte, error)
```

序号有效范围为 `0–15`，通常从 `1` 开始，`15` 之后回绕到 `0`。

```go
frame, err := tp_layer.CreateConsecutiveFrame(
	[]byte{7, 8, 9},
	1,
	false,
)
// frame: 21 07 08 09
```

Classic CAN 的 `dataChunk` 最多为 7 字节，CAN FD 最多为 63 字节。

## 通过 Preset 直接发送 TP 帧

Preset 提供对应的编码并发送接口。这些方法不依赖 `p.Client`：

```go
func (p *Preset) WriteTPSingleFrame(
	id int32,
	fd bool,
	data []byte,
) error

func (p *Preset) WriteTPFirstFrame(
	id int32,
	fd bool,
	firstChunk []byte,
	totalMessageSize int,
) error

func (p *Preset) WriteTPFlowControlFrame(
	id int32,
	fd bool,
	status tp_layer.FlowStatus,
	blockSize int,
	stMin byte,
) error

func (p *Preset) WriteTPConsecutiveFrame(
	id int32,
	fd bool,
	dataChunk []byte,
	sequenceNumber int,
) error
```

示例：

```go
// Single Frame
err := p.WriteTPSingleFrame(
	0x7C6, false,
	[]byte{0x22, 0xF1, 0x90},
)

// First Frame
err = p.WriteTPFirstFrame(
	0x7C6, false,
	[]byte{1, 2, 3, 4, 5, 6},
	20,
)

// Flow Control/CTS
err = p.WriteTPFlowControlFrame(
	0x7C6, false,
	tp_layer.FlowStatusContinueToSend,
	8,
	0x05,
)

// Consecutive Frame, SN=1
err = p.WriteTPConsecutiveFrame(
	0x7C6, false,
	[]byte{7, 8, 9},
	1,
)
```

调用方负责控制首帧、流控帧、连续帧之间的等待时间、BlockSize 和序号递增。

## TP 帧填充开关

填充只影响 `Preset.WriteTP*` 方法，默认关闭。开启后，短于 8 字节的 TP 帧会补到
8 字节；已经达到 8 字节或更长的 CAN FD 帧保持不变。

默认填充值为 `0xAA`。

### 构造时配置

```go
func WithTPPadding(enabled bool) preset.Option
func WithTPPaddingByte(paddingByte byte) preset.Option
```

```go
p, err := preset.NewPresetToomoss(
	0x7C6, 0x7C7, 0x7DF,
	driver.CHANNEL1, driver.CANFD,
	preset.WithTPPadding(true),
	preset.WithTPPaddingByte(0xAA),
)
```

`WithTPPaddingByte` 只设置填充值，不会自动开启填充。

### 运行时配置

```go
func (p *Preset) SetTPPadding(enabled bool)
func (p *Preset) SetTPPaddingByte(paddingByte byte)
func (p *Preset) TPPadding() (enabled bool, paddingByte byte)
```

```go
p.SetTPPadding(true)
p.SetTPPaddingByte(0x00)

enabled, paddingByte := p.TPPadding()

p.SetTPPadding(false)
```

填充示例：

```text
未填充流控帧：30 08 05
0xAA 填充后：30 08 05 AA AA AA AA AA
0x00 填充后：30 08 05 00 00 00 00 00
```

直接调用 `tp_layer.Create*` 不应用 Preset 的填充设置。如果需要只编码再填充，可以
修改返回的 `[]byte`，然后通过 `Preset.Write` 发送。

## 手动首帧、流控和连续帧流程

下面的流程完全不使用 UDS client：

```go
p, err := preset.NewPresetToomoss(
	0x7C6, 0x7C7, 0x7DF,
	driver.CHANNEL1, driver.CAN,
	preset.WithTPPadding(true),
)
if err != nil {
	return err
}
defer p.Close()

// 1. 手动发送 First Frame。
err = p.WriteTPFirstFrame(
	0x7C6, false,
	[]byte{1, 2, 3, 4, 5, 6},
	20,
)
if err != nil {
	return err
}

// 2. 通过 Read() 等待并自行解析对端 Flow Control。
raw := <-p.Read()
length, valid := raw.ValidDataLength()
if !valid {
	return fmt.Errorf("invalid CAN DLC: %d", raw.DLC)
}
parsed, err := tp_layer.ParseFrame(&tp_layer.CanMessage{
	ArbitrationID: raw.ID,
	Data:          raw.Data[:length],
	IsFD:          raw.IsFD,
})
if err != nil {
	return err
}
fc, ok := parsed.(*tp_layer.FlowControlFrame)
if !ok || fc.FlowStatus != tp_layer.FlowStatusContinueToSend {
	return fmt.Errorf("expected Flow Control/CTS")
}

// 3. 调用方按照 fc.BlockSize 和 fc.STmin 控制连续帧发送。
time.Sleep(fc.STmin)
err = p.WriteTPConsecutiveFrame(
	0x7C6, false,
	[]byte{7, 8, 9, 10, 11, 12, 13},
	1,
)
```

## DLL 导出建议

这些新增 Go API 均为公开接口，可以在 `c-shared` 项目中包装为 C ABI。建议保持：

- CAN ID 使用 `int32_t` 或 `uint32_t`
- `fd`、`enabled` 使用 `uint8_t`
- FlowStatus、BlockSize、STmin、SequenceNumber 使用固定宽度整数
- 数据使用调用方持有的 `uint8_t *` 和长度参数
- Go `error` 映射为已有的 FFI 状态码，并通过 `preset_last_error` 返回详情
- 不要跨 C ABI 暴露 Go slice、interface、channel 或 Go 指针

建议的 DLL 函数命名：

```text
preset_set_tp_padding
preset_get_tp_padding
preset_tp_write_single_frame
preset_tp_write_first_frame
preset_tp_write_flow_control_frame
preset_tp_write_consecutive_frame
```

