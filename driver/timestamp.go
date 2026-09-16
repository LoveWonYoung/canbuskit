package driver

func toomossTimestampUS(high byte, low uint32, tickUS uint64) uint64 {
	ticks := uint64(high)<<32 | uint64(low)
	return ticks * tickUS
}
