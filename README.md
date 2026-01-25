# canbuskit

canbuskit 是一个面向 CAN/UDS 的 Go 工具库，包含 ISO-TP 传输层、UDS 客户端与常用服务封装，并提供 Windows 下的硬件驱动适配。

## 功能
- ISO-TP 传输层（tp_layer）：支持 CAN/CAN-FD、寻址模式切换、超时与流控配置。
- UDS 客户端（uds_client）：支持重试、Response Pending (0x78) 处理、物理/功能寻址切换。
- 驱动适配（driver）：统一 CAN 驱动接口，内置 TSMaster/Toomoss Windows 驱动与自动选择器。
- 服务封装（services）：ReadDataByIdentifier、RoutineControl、RequestDownload/TransferData/RequestTransferExit、SecurityAccess 等。
- Intel HEX 解析与分块发送辅助。

## 结构
- `driver/`：CAN 硬件驱动适配与统一接口。
- `tp_layer/`：ISO-TP 传输层实现与测试。
- `uds_client/`：UDS 客户端实现与测试。
- `services/`：常见 UDS 服务封装与 HEX 解析工具。

## 快速开始

### 安装
```bash
go get github.com/LoveWonYoung/canbuskit
```

### 示例：读取 DID
```go
package main

import (
	"log"

	"github.com/LoveWonYoung/canbuskit/driver"
	"github.com/LoveWonYoung/canbuskit/services"
	"github.com/LoveWonYoung/canbuskit/tp_layer"
	"github.com/LoveWonYoung/canbuskit/uds_client"
)

func main() {
	addr, err := tp_layer.NewAddress(tp_layer.Normal11Bit,
		tp_layer.WithTxID(0x7E0),
		tp_layer.WithRxID(0x7E8),
	)
	if err != nil {
		log.Fatal(err)
	}

	cfg := tp_layer.DefaultConfig()

	dev := driver.NewAutoDriver(driver.CAN)
	client, err := uds_client.NewUDSClient(dev, addr, cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	rdbi := services.NewReadDataByIdentifier(client)
	resp, err := rdbi.ReadDataByIdentifier(0xF190)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("DID 0xF190: % X", resp.Values[0xF190])
}
```

## UDS 服务封装
- `ReadDataByIdentifier`：支持多 DID 与固定长度解析。
- `RoutineControl`：例程控制请求与响应解析。
- `RequestDownload` / `TransferData` / `RequestTransferExit`：下载与传输流程。
- `SecurityAccess`：Windows 下基于 `SecKey.dll` 计算 Key。

## 平台与硬件
- `driver/` 内置驱动为 Windows 构建（`//go:build windows`）。
- 非 Windows 平台请自行实现 `driver.CANDriver` 接口并注入。
- `SecurityAccess` 在非 Windows 下返回 `ErrSecKeyUnsupported`。

## 构建与测试
```bash
go build ./...
go test ./...
go test ./tp_layer -bench .
go test ./uds_client -run TestName
```

## 许可证
见 `LICENSE`。
