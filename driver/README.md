# wsbridge

`wsbridge` 是一个单独目录下的 `canbuskit` 驱动实现，用 websocket 代替实体 CAN 硬件。

它复用了 `vertor-bridge` 现有的 websocket 协议：

- 握手：`hello` / `hello_ack`
- 数据帧：`can_frame`
- 鉴权：固定 `token` 或 `hmac_sha256`

这个驱动适合两端都接入同一 websocket relay 的场景。对 `canbuskit` 上层来说，它表现为标准 `CANDriver`，所以可以直接配合 `driver.NewAdapter`、`uds_client.NewUDSClient` 使用。

## 用法

```go
package main

import (
	"log"

	"github.com/LoveWonYoung/canbuskit/driver"
)

func main() {
	dev := driver.WSBridgeNew(
		driver.CANFD,
		driver.Config{
			ServerURL: "ws://127.0.0.1:8898/ws",
			BridgeID:  "demo-bridge",
			Side:      "left",
			AuthToken: "your-token",
			Channel:   0,
		},
	)

	if err := dev.Init(); err != nil {
		log.Fatal(err)
	}
	dev.Start()
	defer dev.Stop()

	if err := dev.Write(0x123, []byte{0x01, 0x02, 0x03}); err != nil {
		log.Fatal(err)
	}
}

```

## 注意

- `Write` 只有 `id + data`，所以这个适配层默认发送标准数据帧，不携带扩展帧/RTR/错误帧等硬件细节。
- websocket 协议里的 `dlc` 字段使用“真实字节数”；进入 `canbuskit` 后会转换成标准 CAN DLC 编码，避免 CAN-FD 长度被上层误判。
- 如果需要和当前 `vertor-bridge` 直接互通，服务端需要兼容它现在的 websocket JSON 协议。
