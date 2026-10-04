# canbuskit

`canbuskit` 是一个 Go 语言 CAN / CAN FD 工具库，提供统一的硬件驱动接口、ISO-TP 传输层和 UDS 客户端。你可以从原始 CAN 帧开始使用，也可以直接发送 UDS 请求。UDS 请求的 SID 与参数由调用方组装，库负责分帧、流控、响应等待和负响应处理。

## 支持范围

| 驱动 | 平台 | 模式 | 构造函数 |
| --- | --- | --- | --- |
| Toomoss | Windows、macOS（需启用 CGO） | CAN / CAN FD | `driver.NewToomoss` |
| TSMaster | Windows | CAN / CAN FD | `driver.NewTSMaster` |
| PCAN | Windows | CAN / CAN FD | `driver.NewPCAN` |
| Vector | Windows | CAN / CAN FD | `driver.NewVector` |
| CanalystII | Windows | 经典 CAN | `driver.NewCanalystII` |
| AutoDriver | Windows | 取决于选中的驱动 | `driver.NewAutoDriver` |

`AutoDriver` 默认依次尝试 Toomoss、TSMaster、PCAN、Vector，选用第一个初始化成功且模式匹配的设备；不会自动尝试 CanalystII。各硬件驱动需要相应厂商的设备和动态库。macOS Toomoss 驱动从 `/Applications/TCANLINPro.app/Contents/Frameworks/` 加载 `libusb-1.0.0.dylib` 与 `libUSB2XXX.dylib`。

目前硬件驱动和 ISO-TP 地址都只支持 **11 位标准 CAN ID（`0x000`–`0x7FF`）**。ISO-TP 使用普通寻址；29 位扩展 ID 不在支持范围内。

## 安装

需要 Go 1.25.4 或更高版本（见 [`go.mod`](go.mod)）：

```bash
go get github.com/LoveWonYoung/canbuskit
```

## 快速开始：发送 UDS 请求

以下示例以 Toomoss CAN FD 设备为例，发送 ReadDataByIdentifier 请求，读取 DID `F190`。`0x7C6` 是请求 ID，`0x7C7` 是响应 ID；请按实际 ECU 和硬件配置修改。

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

`NewUDSClient` 会初始化并启动驱动；`Close` 会停止客户端及其持有的驱动。Windows 下如果希望自动选择设备，可把构造驱动的一行改为 `dev := driver.NewAutoDriver(driver.CANFD)`。

常用的 UDS 接口还有 `RequestWithTimeout`、`RequestWithContext`、`SetFunctionalAddress` 和 `SendAndRecvWithAddressingMode`。功能寻址要先设置功能地址，例如 `isotp.NewAddress(0x7DF, 0x7C7)`；请求时仍从指定响应 ID 接收。`Request` 会处理 `0x7F` 负响应、`0x78 Response Pending` 和可重试的负响应。需要控制超时及重试时，使用 `uds_client.RequestOptions`。

## 原始 CAN 帧与 Preset

`preset` 帮你构造并启动设备。**默认不创建 UDS 客户端**，适合直接收发 CAN 帧：

```go
p, err := preset.NewPresetToomoss(0x7C6, 0x7C7, 0x7DF, driver.CHANNEL1, driver.CAN)
if err != nil {
	log.Fatal(err)
}
defer p.Close()

if err := p.Write(0x7C6, false, []byte{0x03, 0x22, 0xF1, 0x90}); err != nil {
	log.Fatal(err)
}
frame := <-p.Read()
fmt.Printf("ID=0x%03X data=% X\n", frame.ID, frame.Data[:frame.DataLength()])
```

上例的 `Write` 发送的是调用方自己编码的原始 CAN 数据。启用 UDS 后，可以让 Preset 负责 ISO-TP 封装：

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

未启用 UDS 时，`Request`、`FunctionRequest`、`SetDefaultBlockSize` 和 `SetDefaultStMin` 返回 `preset.ErrUDSClientDisabled`。

需要自行控制 ISO-TP 帧的发送时，可以使用 `preset.WriteTPSingleFrame`、`WriteTPFirstFrame`、`WriteTPFlowControlFrame`、`WriteTPConsecutiveFrame`；它们不需要启用 UDS。只编码不发送时，使用 `tp_layer.CreateSingleFrame` 等对应函数。手动帧填充默认关闭，可通过 `preset.WithTPPadding(true)` 开启 8 字节填充。参数、STmin 编码和完整流程见 [Preset 手动 ISO-TP API](docs/preset_tp_api.md)。

## 驱动与传输层配置

`driver.DefaultConfig(mode, channel)` 默认使用 500 kbit/s 仲裁速率和 2 Mbit/s 数据速率，通道由调用方指定。需要自定义时，修改 `driver.Config`：

```go
cfg := driver.DefaultConfig(driver.CANFD, driver.CHANNEL2)
cfg.NominalBitrate = 500_000
cfg.DataBitrate = 4_000_000
cfg.BRS = true // CAN FD 发送帧启用位速率切换；默认 false

dev := driver.NewToomossWithConfig(cfg)
```

Windows 下也有 `NewTSMasterWithConfig(cfg, driver.TC1016)`、`NewPCANWithConfig(cfg)`、`NewVectorWithConfig(cfg, driver.CANOEVN1640)`、`NewCanalystIIWithConfig(cfg)` 和 `NewAutoDriverWithConfig(cfg)`。CanalystII 只接受 `driver.CAN` 模式。`CHANNEL1` 从 0 开始计数；TSMaster 的 `cfg.Channel` 指物理通道，复杂设备映射可使用 `NewTSMasterWithMapping`。

`driver.Write(id, fd, data)` 用 `fd` 指定发送经典 CAN 帧还是 CAN FD 帧。经典 CAN 最多 8 字节，CAN FD 最多 64 字节；经典 CAN 模式的驱动不能发送 CAN FD 帧。`driver.Config.IncludeTxEcho` 默认是 `false`，开启后设备提供的 TX 回显也会进入 `RxChan`。

ISO-TP 配置从 `isotp.DefaultConfig()` 开始修改，包括 `TimeoutN_Bs`、`TimeoutN_Cr`、`BlockSize`、`StMin`、`PaddingByte`、`MaxPayloadSize` 和 `MaxWaitFrames`。默认最大载荷为 16 MiB。`StMin` 配置值单位是毫秒（0–127）；手动创建流控帧时传入的是 ISO-TP 原始 STmin 字节。

## 日志与运行状态

帧日志默认关闭。需要查看收发帧时：

```go
driver.SetPrintLog(true)
_ = driver.SetLogFilter(driver.LogFilterList, []uint32{0x7C6, 0x7C7})
```

日志中的 `Elapsed` 和 `Delta` 是相对时间。`driver.CanFrame.TimestampUS` 保存设备提供的微秒级硬件时间戳；值为 0 表示没有可用的硬件时间戳。设备的 TX 回显与时间戳能力各不相同；`IncludeTxEcho` 只控制 TX 回显是否进入接收通道，不影响帧日志。

内置驱动实现 `driver.ObservableCANDriver`，可通过 `Errors()`、`Stats()` 和 `BusLoad()` 读取异步错误、接收队列丢帧统计及总线负载。UDS 客户端的 `Errors()` 提供异步 ISO-TP 错误。

## 测试与许可

```bash
go test ./...
```

macOS 和 Windows 的硬件驱动由平台编译条件控制，实际通信需要对应设备及厂商库。项目采用 [MIT 许可证](LICENSE)。
