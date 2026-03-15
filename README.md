# canbuskit

`canbuskit` 是一个面向 Go 的 CAN / CAN FD 诊断工具库，提供了：

- 多种底层 CAN 驱动封装
- ISO-TP 传输层实现
- UDS 客户端
- 常见 UDS 服务封装
- 面向刷写场景的 HEX / SREC 分块辅助能力

项目适合做 ECU 诊断、刷写、自动化测试，以及把不同 CAN 硬件接入统一的 Go 接口。

## 模块结构

仓库主要分成四层：

- `driver`：底层 CAN 驱动统一接口，屏蔽不同厂商设备差异
- `tp_layer`：ISO-15765-2 传输层，实现单帧、多帧、流控、超时管理
- `uds_client`：基于 `driver + ISO-TP` 的 UDS 客户端，负责请求、超时、负响应和重试逻辑
- `services`：对常见 UDS 服务做了更高层封装

如果现成服务不够用，也可以直接调用 `UDSClient.Request(...)` 发送任意 SID。

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

### 远程桥接驱动

- `driver/wsbridge`
  - 通过 WebSocket 对接远端 CAN relay
  - 对上层暴露标准 `driver.CANDriver`
  - 适合无本地硬件、跨机诊断或桥接测试环境

`wsbridge` 的详细说明见 [driver/wsbridge/README.md](/Users/lianmin/Documents/GitHub/canbuskit/driver/wsbridge/README.md)。

## 安装

```bash
go get github.com/LoveWonYoung/canbuskit
```

## 快速开始

下面示例演示一个典型链路：

`CAN Driver -> Adapter -> ISO-TP -> UDS Client -> UDS Service`

```go
package main

import (
	"fmt"
	"log"

	"github.com/LoveWonYoung/canbuskit/driver"
	"github.com/LoveWonYoung/canbuskit/services"
	isotp "github.com/LoveWonYoung/canbuskit/tp_layer"
	"github.com/LoveWonYoung/canbuskit/uds_client"
)

func main() {
	dev := driver.NewToomoss(driver.CANFD, driver.CHANNEL1)

	addr, err := isotp.NewAddress(
		isotp.Normal11Bit,
		isotp.WithTxID(0x7C6),
		isotp.WithRxID(0x7C7),
	)
	if err != nil {
		log.Fatal(err)
	}

	client, err := uds_client.NewUDSClient(dev, addr, isotp.DefaultConfig())
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	client.SetFDMode(true)

	rdbi := services.NewReadDataByIdentifier(client)
	resp, err := rdbi.ReadDataByIdentifier(0xF190)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("VIN: %X\n", resp.Values[0xF190])
}
```

如果你在 Windows 下希望自动挑选本机可用设备，可以把驱动替换成：

```go
dev := driver.NewAutoDriver(driver.CANFD)
```

如果你不直接连硬件，而是走 WebSocket bridge，可以使用：

```go
package main

import (
	"log"

	canbusdriver "github.com/LoveWonYoung/canbuskit/driver"
	"github.com/LoveWonYoung/canbuskit/driver/wsbridge"
)

func main() {
	dev := wsbridge.New(canbusdriver.CANFD, wsbridge.Config{
		ServerURL: "ws://127.0.0.1:8898/ws",
		BridgeID:  "demo-bridge",
		Side:      "left",
		AuthToken: "your-token",
		Channel:   0,
	})

	if err := dev.Init(); err != nil {
		log.Fatal(err)
	}
	dev.Start()
	defer dev.Stop()
}
```

## 寻址与 ISO-TP 配置

`tp_layer` 支持多种寻址模式：

- `Normal11Bit`
- `Normal29Bit`
- `NormalFixed29Bit`
- `Extended11Bit`
- `Extended29Bit`
- `Mixed11Bit`
- `Mixed29Bit`

基础配置来自：

```go
cfg := isotp.DefaultConfig()
```

你可以按需覆盖：

- `PaddingByte`
- `TimeoutN_As / N_Bs / N_Cs`
- `TimeoutN_Ar / N_Br / N_Cr`
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
- CAN / CAN FD 切换

常用方法：

- `Request(payload []byte)`
- `RequestWithTimeout(payload, timeout)`
- `RequestWithContext(ctx, payload, opts)`
- `SendAndRecv(payload, timeout)`
- `SetFDMode(isFD bool)`
- `SetFunctionalAddress(addr)`
- `UseFunctionalAddress()`
- `UsePhysicalAddress()`

例如，直接发送一个未封装的 UDS 请求：

```go
resp, err := client.Request([]byte{0x10, 0x03})
```

## 已封装的 UDS 服务

`services` 目录目前包含：

- `ReadDataByIdentifier` (`0x22`)
- `RoutineControl` (`0x31`)
- `RequestDownload` (`0x34`)
- `TransferData` (`0x36`)
- `RequestTransferExit` (`0x37`)
- `SecurityAccess` (`0x27`)

示例：读取多个 DID

```go
rdbi := services.NewReadDataByIdentifier(client)

resp, err := rdbi.ReadDataByIdentifierWithLengths(
	map[uint16]int{
		0xF187: 16,
		0xF190: 17,
	},
	0xF187,
	0xF190,
)
if err != nil {
	log.Fatal(err)
}

fmt.Printf("DID F187: %X\n", resp.Values[0xF187])
fmt.Printf("DID F190: %X\n", resp.Values[0xF190])
```

## 刷写流程示例

仓库已经提供了刷写链路里最常见的几个步骤封装：

1. `RequestDownload`
2. `TransferData`
3. `RequestTransferExit`

同时支持把 HEX / SREC 文件解析成分段和分块。

```go
reqDownload := services.NewRequestDownload(client)
transfer := services.NewTransferData(client)
exit := services.NewRequestTransferExit(client)

downloadResp, err := reqDownload.RequestDownload(0x00100000, 0x00002000, 4, 4)
if err != nil {
	log.Fatal(err)
}

fmt.Printf("ECU max block len: %d\n", downloadResp.MaxLength)

_, nextSeq, err := transfer.TransferHexFile("./app.hex", 256, 1)
if err != nil {
	log.Fatal(err)
}

fmt.Printf("next sequence: 0x%02X\n", nextSeq)

_, err = exit.RequestTransferExit(nil)
if err != nil {
	log.Fatal(err)
}
```

如果你只想解析文件，不立刻发送，也可以直接使用：

- `ParseHexSegments`
- `MyHexParser`
- `MyHexParserWithLengths`

支持按扩展名或内容自动识别：

- Intel HEX
- SREC / S19 / S28 / S37

## SecurityAccess 说明

`services.SecurityAccess` 在不同平台行为不同：

- Windows：通过 `SecKey.dll` 加载 `SecKeyCmac` 计算 key
- 非 Windows：提供 stub，实现会返回不支持错误

如果你的项目依赖 `SecurityAccess`，需要自行准备匹配 ECU 算法的 `SecKey.dll`。

## 注意事项

- `driver` 层只提供统一的 `Write(id, data)` 能力，不直接暴露扩展帧、RTR、错误帧等更细的硬件细节。
- `wsbridge` 为了兼容桥接协议，`dlc` 使用真实字节长度，在库内部会转换为标准 DLC。
- `services` 只封装了部分常见 UDS 服务；其他服务建议直接用 `UDSClient.Request(...)`。
- `UDSClient.Close()` 会同时关闭后台 goroutine 和底层设备连接，使用结束后应主动调用。

## 测试

```bash
go test ./...
```

当前仓库已经包含 `tp_layer`、`uds_client`、`driver/wsbridge` 的测试。

## License

[MIT]
