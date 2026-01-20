package driver

import (
	"errors"
	"fmt"
	"log"

	"github.com/LoveWonYoung/canbuskit/tp_layer"
)

// _toomoss_adapter.go (与 main.go 放在同一目录下)

// ToomossAdapter 是连接 go-uds 和 Toomoss 硬件的适配器
type ToomossAdapter struct {
	driver CANDriver // 使用接口，使其可以同时支持 CAN 和 CAN-FD
	rxChan <-chan UnifiedCANMessage
}

// NewToomossAdapter 是适配器的构造函数
func NewToomossAdapter(dev CANDriver) (*ToomossAdapter, error) {
	if dev == nil {
		return nil, errors.New("CAN driver instance cannot be nil")
	}
	if err := dev.Init(); err != nil {
		return nil, fmt.Errorf("failed to initialize toomoss device: %w", err)
	}
	dev.Start()

	adapter := &ToomossAdapter{
		driver: dev,
		rxChan: dev.RxChan(),
	}

	log.Println("Toomoss-Adapter created and device started successfully.")
	return adapter, nil
}

// Close 用于停止驱动并释放资源
func (t *ToomossAdapter) Close() {
	log.Println("Closing Toomoss-Adapter...")
	t.driver.Stop()
}

// TxFunc 是发送函数，它完全符合 go-uds 库的 `txfn` 签名要求
func (t *ToomossAdapter) TxFunc(msg tp_layer.CanMessage) {
	err := t.driver.Write(int32(msg.ArbitrationID), msg.Data)
	if err != nil {
		log.Printf("ERROR: ToomossAdapter failed to send message: %v", err)
	}
}

// RxFunc 是接收函数，它完全符合 go-uds 库的 `rxfn` 签名要求
func (t *ToomossAdapter) RxFunc() (tp_layer.CanMessage, bool) {
	select {
	case receivedMsg, ok := <-t.rxChan:
		if !ok {
			// 如果通道已关闭，返回false
			return tp_layer.CanMessage{}, false
		}

		// 使用 DLC 转换表获取实际数据长度（CAN-FD 下 DLC 不是直接长度）。
		payloadLength := dlcToLen(receivedMsg.DLC)

		// 添加一个安全检查，防止因DLC值错误导致的panic
		if payloadLength > len(receivedMsg.Data) {
			log.Printf("警告: 收到的报文DLC (%d) 大于数据数组长度 (%d)。ID: 0x%X", receivedMsg.DLC, len(receivedMsg.Data), receivedMsg.ID)
			payloadLength = len(receivedMsg.Data) // 使用数组实际长度作为安全保障
		}

		// 正确创建 tp_layer.CanMessage
		// 我们假设 UnifiedCANMessage 结构体中包含了 IsExtendedID, IsFD, BitrateSwitch 字段
		isotpMsg := tp_layer.CanMessage{
			ArbitrationID: receivedMsg.ID,
			// 【关键修正1】: 使用正确的长度来切片数据
			Data: receivedMsg.Data[:payloadLength],
			// 【关键修正2】: 从接收到的消息中映射正确的标志位
			IsExtendedID:  false,
			IsFD:          receivedMsg.IsFD,
			BitrateSwitch: false,
		}

		return isotpMsg, true

	default:
		// 通道中无可用消息
		// 【关键修正3】: 返回正确的新类型
		return tp_layer.CanMessage{}, false
	}
}
