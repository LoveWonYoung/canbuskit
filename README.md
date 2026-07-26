# canbuskit

`canbuskit` 是一个面向 Go 的 CAN / CAN FD 诊断工具库，提供了：

- 多种底层 CAN 驱动封装
- ISO-TP 传输层实现
- UDS 客户端

项目适合做 ECU 诊断、刷写、自动化测试，以及把不同 CAN 硬件接入统一的 Go 接口。

当前硬件驱动统一支持标准 11 位 ID 的 CAN / CAN FD 数据帧；29 位扩展帧不在驱动层支持范围内。

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

`IncludeTxEcho` 默认为 `false`。抓包程序如果需要同时观察发送帧，可以显式开启；UDS 客户端始终只处理 RX 帧。

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

例如，直接发送一个未封装的 UDS 请求：

```go
resp, err := client.Request([]byte{0x10, 0x03})
```

## 注意事项

- `driver` 层只提供统一的 `Write(id, fd, data)` 能力，通过 `fd` 标志在同一函数里发送 CAN / CAN-FD。
- 驱动层只接受 `0x000-0x7FF` 的标准 11 位 CAN ID。
- UDS 服务请求由调用方通过 `UDSClient.Request(...)` 直接组装。
- `UDSClient.Close()` 会同时关闭后台 goroutine 和底层设备连接，使用结束后应主动调用。

## 测试

```bash
go test ./...
```

当前仓库已经包含 `tp_layer`、`uds_client` 的测试。

## License

[MIT]
