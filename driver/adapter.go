package driver

import (
	"errors"
	"fmt"
	"log"

	"github.com/LoveWonYoung/canbuskit/tp_layer"
)

// ToomossAdapter 是连接 go-uds 和 Toomoss 硬件的适配器
type ToomossAdapter struct {
	driver CANDriver
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

// TxFunc 是发送函数
func (t *ToomossAdapter) TxFunc(msg tp_layer.CanMessage) {
	err := t.driver.Write(int32(msg.ArbitrationID), msg.Data)
	if err != nil {
		log.Printf("ERROR: ToomossAdapter failed to send message: %v", err)
	}
}

// RxFunc 是接收函数
func (t *ToomossAdapter) RxFunc() (tp_layer.CanMessage, bool) {
	select {
	case receivedMsg, ok := <-t.rxChan:
		if !ok {
			return tp_layer.CanMessage{}, false
		}
		payloadLength := dlcToLen(receivedMsg.DLC)
		if payloadLength > len(receivedMsg.Data) {
			log.Printf("警告: 收到的报文DLC (%d) 大于数据数组长度 (%d)。ID: 0x%X", receivedMsg.DLC, len(receivedMsg.Data), receivedMsg.ID)
			payloadLength = len(receivedMsg.Data) // 使用数组实际长度作为安全保障
		}

		isotpMsg := tp_layer.CanMessage{
			ArbitrationID: receivedMsg.ID,
			Data: receivedMsg.Data[:payloadLength],
			IsExtendedID:  false,
			IsFD:          receivedMsg.IsFD,
			BitrateSwitch: false,
		}

		return isotpMsg, true

	default:
		return tp_layer.CanMessage{}, false
	}
}
