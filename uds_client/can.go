package uds_client

import (
	"github.com/LoveWonYoung/canbuskit/driver"
	isotp "github.com/LoveWonYoung/canbuskit/tp_layer"
)

func convertRXMessage(raw driver.CanFrame) (isotp.CanMessage, bool) {
	if raw.Direction == driver.TX || raw.ID > 0x7FF {
		return isotp.CanMessage{}, false
	}

	length := raw.DataLength()
	data := make([]byte, length)
	copy(data, raw.Data[:length])

	return isotp.CanMessage{
		ArbitrationID: raw.ID,
		Data:          data,
		IsFD:          raw.IsFD,
	}, true
}
