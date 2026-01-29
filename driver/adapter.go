package driver

import (
	"errors"
	"fmt"
	"log"

	"github.com/LoveWonYoung/canbuskit/tp_layer"
)

// Adapter is link udsclient hardware adapter of can device
type Adapter struct {
	driver CANDriver
	rxChan <-chan UnifiedCANMessage
}

// NewAdapter is adapter create
func NewAdapter(dev CANDriver) (*Adapter, error) {
	if dev == nil {
		return nil, errors.New("CAN driver instance cannot be nil")
	}
	if err := dev.Init(); err != nil {
		return nil, fmt.Errorf("failed to initialize CAN device: %w", err)
	}
	dev.Start()

	adapter := &Adapter{
		driver: dev,
		rxChan: dev.RxChan(),
	}

	log.Println("CAN adapter created and device started successfully.")
	return adapter, nil
}

// Close stop driver
func (t *Adapter) Close() {
	log.Println("Closing CAN adapter...")
	t.driver.Stop()
}

// TxFunc is send function
func (t *Adapter) TxFunc(msg tp_layer.CanMessage) {
	err := t.driver.Write(int32(msg.ArbitrationID), msg.Data)
	if err != nil {
		log.Printf("ERROR: Adapter failed to send message: %v", err)
	}
}

// RxFunc is receive function
func (t *Adapter) RxFunc() (tp_layer.CanMessage, bool) {
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
			Data:          receivedMsg.Data[:payloadLength],
			IsExtendedID:  false,
			IsFD:          receivedMsg.IsFD,
			BitrateSwitch: false,
		}

		return isotpMsg, true

	default:
		return tp_layer.CanMessage{}, false
	}
}
