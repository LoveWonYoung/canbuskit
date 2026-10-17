package uds_client

import (
	"github.com/LoveWonYoung/canbuskit/v2/driver"
	isotp "github.com/LoveWonYoung/canbuskit/v2/tp_layer"
)

func convertRXMessage(raw driver.CanFrame) (isotp.CanMessage, bool) {
	if raw.Direction == driver.TX || raw.ID > 0x7FF {
		return isotp.CanMessage{}, false
	}

	length, valid := raw.ValidDataLength()
	if !valid {
		return isotp.CanMessage{}, false
	}
	data := make([]byte, length)
	copy(data, raw.Data[:length])

	return isotp.CanMessage{
		ArbitrationID: raw.ID,
		Data:          data,
		IsFD:          raw.IsFD,
	}, true
}
