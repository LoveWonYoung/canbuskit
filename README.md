# canbuskit

`canbuskit` 是一个面向 Windows CAN / CAN FD 硬件的 Go 工具库，包含统一总线接口、ISO-TP 会话和 UDS 客户端。v2 当前 API 是一次破坏性重构：应用负责硬件生命周期，协议层不再隐式打开或关闭设备。

## 包结构

| 包 | 用途 |
| --- | --- |
| `can` | 平台无关的帧、配置和驱动接口 |
| `can/memory` | 测试和仿真使用的双端内存 CAN 总线 |
| `driver/toomoss` | Toomoss CAN / CAN FD 后端 |
| `driver/tsmaster` | TSMaster CAN / CAN FD 后端 |
| `driver/pcan` | PCAN CAN / CAN FD 后端 |
| `driver/vector` | Vector CAN / CAN FD 后端 |
| `driver/canalystii` | Canalyst-II 经典 CAN 后端 |
| `driver/auto` | 按优先级自动探测 Windows 后端 |
| `isotp` | ISO-TP 编解码和会话状态机 |
| `uds` | UDS 请求、响应、超时与负响应处理 |

所有真实硬件后端仅支持 Windows，厂商结构体、DLL 加载器和 syscall 细节均位于 `internal`，不会成为公共 API。当前硬件实现只发送 11 位标准 CAN ID；`can.Frame` 已保留 `Extended` 标志，但 Windows 后端会明确拒绝尚未支持的 29 位发送。

## 安装

需要 Go 1.25.4 或更高版本：

```bash
go get github.com/LoveWonYoung/canbuskit/v2
```

## UDS 请求

以下示例使用 Toomoss CAN FD。调用方先打开总线，最后按“客户端、总线”的顺序关闭：

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/LoveWonYoung/canbuskit/v2/can"
	"github.com/LoveWonYoung/canbuskit/v2/driver/toomoss"
	"github.com/LoveWonYoung/canbuskit/v2/isotp"
	"github.com/LoveWonYoung/canbuskit/v2/uds"
)

func main() {
	ctx := context.Background()
	cfg := can.DefaultConfig(can.FD, can.Channel1)
	bus := toomoss.New(cfg)
	if err := bus.Open(ctx); err != nil {
		log.Fatal(err)
	}
	defer bus.Close()

	addr, err := isotp.NewAddress(0x7C6, 0x7C7)
	if err != nil {
		log.Fatal(err)
	}
	client, err := uds.New(bus, addr, isotp.DefaultConfig())
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	resp, err := client.Request([]byte{0x22, 0xF1, 0x90})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("response: % X\n", resp)
}
```

功能寻址通过 `client.SetFunctionalAddress` 配置，再调用 `RequestWithContextAndAddressingMode`。UDS 客户端串行化请求，并处理 `0x7F` 负响应、`0x78 Response Pending`、重试和调用方 Context 取消。

## 原始 CAN 帧

`Send` 接受完整帧，不再使用 `id, fd, data` 形式的位置参数：

```go
frame, err := can.NewFrame(
	0x7C6,
	[]byte{0x03, 0x22, 0xF1, 0x90},
	can.FrameFlags{FD: true, BRS: true},
)
if err != nil {
	log.Fatal(err)
}
if err := bus.Send(ctx, frame); err != nil {
	log.Fatal(err)
}

frames, unsubscribe, err := bus.Subscribe(128)
if err != nil {
	log.Fatal(err)
}
defer unsubscribe()
received := <-frames
fmt.Printf("ID=0x%03X data=% X\n", received.ID, received.Payload())
```

`Subscribe` 返回独立订阅及释放函数。慢订阅不会阻塞硬件读取；丢帧数量可从 `bus.Stats()` 获取，异步错误由 `bus.Errors()` 提供。

## ISO-TP 配置

ISO-TP 配置在 `isotp.New` 或 `uds.New` 时复制，创建后不再通过 setter 改写：

```go
tpConfig := isotp.DefaultConfig()
tpConfig.BlockSize = 8
tpConfig.STMin = 5
tpConfig.ManualFlowControl = false

session, err := isotp.New(bus, addr, tpConfig)
```

配置还包括 `FlowControlTimeout`、`ConsecutiveFrameTimeout`、`PaddingByte`、`MaxPayloadSize` 和 `MaxWaitFrames`。

只需要手动编解码帧时，可使用 `isotp.EncodeSingleFrame`、`EncodeFirstFrame`、`EncodeConsecutiveFrame`、`EncodeFlowControlFrame` 和 `ParseFrame`，不需要创建会话。

## 内存总线

协议测试不需要厂商 DLL：

```go
tester, ecu := memory.Pair(can.DefaultConfig(can.FD, can.Channel1))
_ = tester.Open(ctx)
_ = ecu.Open(ctx)
defer tester.Close()
defer ecu.Close()
```

一端发送的帧会进入另一端的所有订阅。`IncludeTxEcho` 打开时，发送端还会收到 TX 回显。

## 日志与状态

库不再修改进程全局 `log` 输出。需要驱动诊断和帧日志时，为配置注入 `slog.Logger`：

```go
cfg := can.DefaultConfig(can.FD, can.Channel1)
cfg.Logger = slog.Default()
```

`bus.BusLoad()` 返回滚动总线负载估计，`bus.Stats()` 返回接收队列丢帧统计。硬件时间戳保存在 `can.Frame.TimestampUS`，0 表示设备没有提供时间戳。

## 测试

```bash
go test ./...
go test -race ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -exec=/usr/bin/true ./...
```

项目采用 [MIT 许可证](LICENSE)。
