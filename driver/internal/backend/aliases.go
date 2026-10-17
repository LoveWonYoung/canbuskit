package backend

import "github.com/LoveWonYoung/canbuskit/v2/can"

type mode = can.Mode
type direction = can.Direction
type config = can.Config
type frame = can.Frame
type driverStats = can.Stats
type busLoadSnapshot = can.BusLoad
type receiveOverflowError = can.ReceiveOverflowError

const (
	CAN   = can.Classic
	CANFD = can.FD
	TX    = can.Transmit
	RX    = can.Receive
)

const (
	CHANNEL1 = can.Channel1
	CHANNEL2 = can.Channel2
	CHANNEL3 = can.Channel3
	CHANNEL4 = can.Channel4
)

var errRxOverflow = can.ErrRxOverflow

func defaultConfig(mode mode, channel byte) config {
	return can.DefaultConfig(mode, channel)
}
