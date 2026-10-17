package driver

import "github.com/LoveWonYoung/canbuskit/v2/can"

type CanType = can.Mode
type DirectionType = can.Direction
type Config = can.Config
type CanFrame = can.Frame
type DriverStats = can.Stats
type BusLoadInfo = can.BusLoad
type ReceiveOverflowError = can.ReceiveOverflowError

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

var ErrRxOverflow = can.ErrRxOverflow

func DefaultConfig(mode CanType, channel byte) Config {
	return can.DefaultConfig(mode, channel)
}
