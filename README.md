# canbuskit

`canbuskit` 是一个面向 Go 的 CAN / CAN FD 诊断工具库，提供了：

- 多种底层 CAN 驱动封装
- ISO-TP 传输层实现
- UDS 客户端

项目适合做 ECU 诊断、刷写、自动化测试，以及把不同 CAN 硬件接入统一的 Go 接口。

当前硬件驱动统一支持标准 11 位 ID 的 CAN / CAN FD 数据帧；29 位扩展帧不在驱动层支持范围内。

`driver.CanFrame.TimestampUS` 统一以微秒保存设备提供的单调硬件时间戳；值为 `0` 表示该帧没有可用的硬件时间戳。不同设备的 TX 回显能力见“日志与硬件时间戳”。

## 模块结构

仓库主要分成三层：

- `driver`：底层 CAN 驱动统一接口，屏蔽不同厂商设备差异
- `tp_layer`：ISO-15765-2 传输层，实现单帧、多帧、流控、超时管理
- `uds_client`：基于 `driver + ISO-TP` 的 UDS 客户端，负责请求、超时、负响应和重试逻辑

通过 `UDSClient.Request(...)` 可以发送任意 UDS SID。

## 已支持的驱动

### 本地硬件驱动

- `driver.NewToomoss(...)`
  - Windows
  - macOS（`darwin && cgo`）
- `driver.NewTSMaster(...)`
  - Windows
- `driver.NewPCAN(...)`
  - Windows
- `driver.NewVector(...)`
  - Windows
- `driver.NewAutoDriver(...)`
  - Windows
  - 按 `Toomoss -> TSMaster -> PCAN -> Vector` 顺序自动选择第一个可用设备

## 安装

```bash
go get github.com/LoveWonYoung/canbuskit
```

## 快速开始

下面示例演示一个典型链路：

`CAN Driver -> ISO-TP -> UDS Client`

```go
package main

import (
	"fmt"
	"log"

	"github.com/LoveWonYoung/canbuskit/driver"
	isotp "github.com/LoveWonYoung/canbuskit/tp_layer"
	"github.com/LoveWonYoung/canbuskit/uds_client"
)

func main() {
	dev := driver.NewToomoss(driver.CANFD, driver.CHANNEL1)

	addr, err := isotp.NewAddress(0x7C6, 0x7C7)
	if err != nil {
		log.Fatal(err)
	}

	client, err := uds_client.NewUDSClient(dev, addr, isotp.DefaultConfig())
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	resp, err := client.Request([]byte{0x22, 0xF1, 0x90})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("response: %X\n", resp)
}
```

如果你在 Windows 下希望自动挑选本机可用设备，可以把驱动替换成：

```go
dev := driver.NewAutoDriver(driver.CANFD)
```

### 驱动配置

旧构造函数默认使用通道 1、500 kbit/s 仲裁速率、2 Mbit/s 数据速率。需要自定义时，可以使用统一的 `driver.Config`：

```go
cfg := driver.DefaultConfig(driver.CANFD, driver.CHANNEL2)
cfg.NominalBitrate = 500_000
cfg.DataBitrate = 4_000_000
cfg.RxBufferSize = 4096
cfg.PollingInterval = 500 * time.Microsecond

dev := driver.NewToomossWithConfig(cfg)
```

Windows 下的其他驱动对应使用：

```go
pcan := driver.NewPCANWithConfig(cfg)
tsmaster := driver.NewTSMasterWithConfig(cfg, driver.TC1016)
vector := driver.NewVectorWithConfig(cfg, driver.CANOEVN1640)
auto := driver.NewAutoDriverWithConfig(cfg)
```

### Preset 模式

`preset` 默认只初始化 CAN 设备，不再自动创建或注册 UDS 客户端。此时直接使用
`Write` 和 `Read` 收发原始 CAN / CAN FD 帧：

```go
p, err := preset.NewPresetToomoss(0x7C6, 0x7C7, 0x7DF, driver.CHANNEL1, driver.CANFD)
if err != nil {
	log.Fatal(err)
}
defer p.Close()

if err := p.Write(0x7C6, true, []byte{0x03, 0x22, 0xF1, 0x90}); err != nil {
	log.Fatal(err)
}
frame := <-p.Read()
```

需要 ISO-TP / UDS 能力时显式打开开关：

```go
p, err := preset.NewPresetToomoss(
	0x7C6, 0x7C7, 0x7DF,
	driver.CHANNEL1, driver.CANFD,
	preset.WithUDSClient(true),
)
if err != nil {
	log.Fatal(err)
}
defer p.Close()

resp, err := p.Request([]byte{0x22, 0xF1, 0x90}, time.Second)
```

未启用 UDS 时调用 `Request`、`FunctionRequest`、`SetDefaultBlockSize` 或
`SetDefaultStMin` 会返回 `preset.ErrUDSClientDisabled`；`SetManualFlowControl`
则保持为空操作。

不启用 UDS client 也可以显式构造并发送单个 ISO-TP 帧。这些接口是无状态的，
不会启动 TP 状态机或增加 RX 订阅，适合手动验证首帧、流控和连续帧时序：

```go
// Single Frame: 03 22 F1 90
err = p.WriteTPSingleFrame(0x7C6, false, []byte{0x22, 0xF1, 0x90})

// First Frame: 10 14 01 02 03 04 05 06
err = p.WriteTPFirstFrame(0x7C6, false, []byte{1, 2, 3, 4, 5, 6}, 20)

// Flow Control/CTS: 30 08 05
err = p.WriteTPFlowControlFrame(
	0x7C6, false, tp_layer.FlowStatusContinueToSend, 8, 0x05,
)

// Consecutive Frame/SN=1: 21 07 08 09
err = p.WriteTPConsecutiveFrame(0x7C6, false, []byte{7, 8, 9}, 1)
```

如果只需要编码、不立即发送，可以直接调用公开的
`tp_layer.CreateSingleFrame`、`CreateFirstFrame`、`CreateFlowControlFrame` 和
`CreateConsecutiveFrame`。这些编码接口不会自动填充字节；需要特殊填充或构造
非标准测试帧时，可以修改返回的 `[]byte` 后再调用 `Preset.Write`。流控接口的
`stMin` 使用 ISO-TP 原始编码，例如 `0x05` 表示 5 ms，`0xF5` 表示 500 μs。

手动 `WriteTP*` 接口的 8 字节填充默认关闭。可以在构造时开启，默认使用
`0xAA` 填充：

```go
p, err := preset.NewPresetToomoss(
	0x7C6, 0x7C7, 0x7DF,
	driver.CHANNEL1, driver.CANFD,
	preset.WithTPPadding(true),
)
```

也可以在运行时切换或修改填充值：

```go
p.SetTPPadding(true)       // 短于 8 字节时补到 8 字节
p.SetTPPaddingByte(0x00)   // 后续改用 0x00 填充
enabled, value := p.TPPadding()
p.SetTPPadding(false)      // 恢复不填充
```

该开关只补齐短于 8 字节的帧；已经达到 8 字节或更长的 CAN FD 帧保持不变。
直接调用 `tp_layer.Create*` 时仍然返回未填充的编码结果。

完整的新增接口、参数约束和手动首帧/流控/连续帧流程见
[`docs/preset_tp_api.md`](docs/preset_tp_api.md)。

对 TSMaster 而言，`cfg.Channel` 表示物理硬件通道。默认会把应用逻辑通道 CAN1 映射到设备索引 0 的该物理通道。例如只连接一个设备但使用物理 CAN4：

```go
cfg := driver.DefaultConfig(driver.CANFD, driver.CHANNEL4)
tsmaster := driver.NewTSMasterWithConfig(cfg, driver.TC1016)
// 映射结果：应用 CAN1 -> 设备 0 / 物理 CAN4
```

需要指定其他应用通道或第 N 个设备时，可以显式配置映射：

```go
mapping := driver.TSMasterMapping{
	ApplicationChannel: driver.CHANNEL2,
	HardwareIndex:      1,
	HardwareChannel:    driver.CHANNEL4,
}
tsmaster := driver.NewTSMasterWithMapping(cfg, driver.TC1016, mapping)
```

## 日志与硬件时间戳

帧日志默认关闭，可以按需开启：

```go
driver.SetPrintLog(true)
```

RX 和具备发送回显能力的 TX 都在设备接收路径统一打印。日志使用设备硬件时间戳计算两个相对时间，并按 `s ms us` 三段显示：

- `Elapsed`：从当前驱动实例收到第一帧有效数据帧开始累计，第一帧为 `0`。
- `Delta`：当前帧与上一帧有效数据帧的硬件时间差，滑动步长为 1，第一帧为 `0`。

相对时间在日志 ID 过滤之前更新，因此 `Delta` 表示设备实际接收的相邻有效帧间隔，而不只是两条可见日志之间的间隔。`CanFrame.TimestampUS` 仍保留厂商提供的原始硬件时间戳，不受日志归零影响。

```text
RX CAN  : Elapsed=0s 000ms 000us, Delta=0s 000ms 000us, ID=0x123, DLC=08, Data=01 02 03 04 05 06 07 08
TX CANFD: Elapsed=0s 000ms 445us, Delta=0s 000ms 445us, ID=0x456, DLC=15, Data=...
```

各驱动的 TX 时间戳行为如下：

- PCAN、TSMaster 和 Vector：TX 日志来自设备发送确认，使用设备返回的硬件时间戳。
- Toomoss CAN FD 模式：根据 `CANFD_MSG.Flags` 的 bit7 判断 TX，时间戳单位为 10 μs；在 CAN FD 模式下发送普通 CAN 帧同样可以正确取得 TX 硬件时间戳。
- Toomoss 标准 CAN 模式：根据 `CAN_MSG.RemoteFlag` 的 bit7 判断 TX，时间戳单位为 100 μs。当前实测的 Toomoss 标准 CAN 接口存在厂商问题，`CAN_SendMsgWithTime` 返回的发送帧没有设置 bit7，因此该帧会按 RX 显示。驱动不会根据 ID 和数据内容推测 TX，以免把其他节点发送的相同报文误判为 TX。

`IncludeTxEcho` 默认为 `false`。它只控制 TX 回显是否进入 `RxChan`，不影响 TX 日志。抓包程序需要同时消费 RX 和 TX 时可以显式开启；UDS 客户端始终只处理 RX 帧。

`BusLoad()` 使用最近 1 秒内帧的估算总线占用时长。带有效 `TimestampUS` 的 RX 帧按设备时间戳进入统计窗口；发送成功的 TX 帧先按主机时间计入，收到匹配的 TX 回显后改按设备时间戳计入，避免重复计数。没有有效硬件时间戳或回显时继续使用主机时间。总线占用时长仍由帧位数和配置波特率估算。

`AutoDriver` 会按默认顺序探测设备，清理初始化失败或模式不匹配的候选。也可以通过 `AutoCandidate` 传入自定义顺序和设备构造参数。

## 寻址与 ISO-TP 配置

Lite 版只支持标准 11 位 CAN ID 的普通寻址。创建连接时直接传入发送 ID 和接收 ID：

```go
addr, err := isotp.NewAddress(0x7C6, 0x7C7)
```

`0x800` 及以上的扩展 ID 会在创建地址时被拒绝。远程帧、错误帧和发送回显不会进入 ISO-TP 接收链路。

基础配置来自：

```go
cfg := isotp.DefaultConfig()
```

你可以按需覆盖：

- `PaddingByte`
- `TimeoutN_Bs`（等待流控帧）
- `TimeoutN_Cr`（等待连续帧）
- `BlockSize`
- `StMin`
- `MaxPayloadSize`（默认 16 MiB，用于限制异常首帧声明导致的内存分配）
- `MaxWaitFrames`（默认 8，用于限制对端连续发送 FlowControl/WAIT）

运行过程中也可以更新后续自动流控帧使用的默认值：

```go
if err := stack.SetDefaultBlockSize(30); err != nil {
	return err
}
if err := stack.SetDefaultStMin(5); err != nil {
	return err
}
```

`BlockSize` 的有效范围是 0–255；当前发送接口中的 `StMin` 单位为毫秒，
有效范围是 0–127。

如果需要通过 driver 的 `Write` 自己发送流控帧，可以关闭 TP 层的自动流控：

```go
stack.SetManualFlowControl(true)

// 收到 First Frame 后，由调用方自行发送 0x30, BlockSize, STmin。
err := dev.Write(int32(addr.TxID), dev.IsFDMode(), []byte{0x30, 0x1E, 0x05})
```

`SetManualFlowControl(false)` 可恢复自动流控；默认即为自动模式。手动模式下，
TP 层仍会维护连续帧接收状态、Block 计数和 N_Cr 超时，但不会自动发送流控帧。
这三个设置方法也由 `UDSClient` 和 `Preset` 原样转发，因此使用预设设备时可以
直接调用 `preset.SetDefaultBlockSize`、`preset.SetDefaultStMin` 和
`preset.SetManualFlowControl`。

## UDS 客户端能力

`uds_client.UDSClient` 负责：

- 请求发送与响应接收
- 超时管理
- `0x7F` 负响应解析
- `0x78 Response Pending` 自动继续等待
- 可重试负响应的有限重试
- 物理地址 / 功能地址切换
- 根据驱动配置自动选择 CAN / CAN FD

常用方法：

- `Request(payload []byte)`
- `RequestWithTimeout(payload, timeout)`
- `RequestWithContext(ctx, payload, opts)`
- `SendAndRecv(payload, timeout)`
- `SetFunctionalAddress(addr)`
- `UseFunctionalAddress()`
- `UsePhysicalAddress()`
- `Errors()`（异步 ISO-TP 错误流，客户端关闭时通道关闭）

例如，直接发送一个未封装的 UDS 请求：

```go
resp, err := client.Request([]byte{0x10, 0x03})
```

`RequestOptions` 还可以通过 `ResponsePendingTimeout` 和
`MaxResponsePending` 控制收到 `0x78 Response Pending` 后的等待时间与最大次数。
客户端会忽略不属于当前请求 SID 的迟到或无关响应，并支持在重试等待和发送队列阻塞时通过
`Context` 及时取消。

## 注意事项

- `driver` 层只提供统一的 `Write(id, fd, data)` 能力，通过 `fd` 标志在同一函数里发送 CAN / CAN-FD。
- 驱动层只接受 `0x000-0x7FF` 的标准 11 位 CAN ID。
- UDS 服务请求由调用方通过 `UDSClient.Request(...)` 直接组装。
- `UDSClient.Close()` 会同时关闭后台 goroutine 和底层设备连接，使用结束后应主动调用。
- `UDSClient.Close()` 可以安全地重复调用；`NewUDSClient` 成功后由客户端持有并负责关闭底层驱动。

## 测试

```bash
go test ./...
```

当前仓库已经包含 `tp_layer`、`uds_client` 的测试。

## License

[MIT]
