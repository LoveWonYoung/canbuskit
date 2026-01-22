package main

import (
	"fmt"
	"runtime"
	"time"

	"github.com/LoveWonYoung/canbuskit/driver"
	"github.com/LoveWonYoung/canbuskit/tp_layer"
	"github.com/LoveWonYoung/canbuskit/uds_client"
)

func main() {
	fmt.Println("GOARCH =", runtime.GOARCH)
	var drv driver.CANDriver = driver.NewAutoDriver(driver.CANFD)
	if err := drv.Init(); err != nil {
		fmt.Printf("Init failed: %v\n", err)
		return
	}
	physAddr, _ := tp_layer.NewAddress(
		tp_layer.Normal11Bit,
		tp_layer.WithTxID(0x5b0),
		tp_layer.WithRxID(0x5b8),
	)
	var client, _ = uds_client.NewUDSClient(drv, physAddr, tp_layer.DefaultConfig())
	// 启动后稍微等待，给硬件初始化的时间
	time.Sleep(1 * time.Second)

	client.SendAndRecv([]byte{0x22, 0xc0, 0x40}, 300)

	// 发送完成后等待一会，确保异步发送队列中的数据都已由DLL发出
	time.Sleep(5 * time.Second)
}
