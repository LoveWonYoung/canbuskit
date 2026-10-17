package driver

type relativeLogClock struct {
	initialized bool
	previousUS  uint64
	elapsedUS   uint64
}

func (c *relativeLogClock) reset() {
	*c = relativeLogClock{}
}

// observe returns the time since the first observed frame and the time since
// the immediately preceding frame. A non-zero wrapPeriodUS unwraps vendor
// counters whose normalized timestamp rolls over at a known interval.
func (c *relativeLogClock) observe(timestampUS, wrapPeriodUS uint64) (elapsedUS, deltaUS uint64) {
	if !c.initialized {
		c.initialized = true
		c.previousUS = timestampUS
		return 0, 0
	}

	if timestampUS >= c.previousUS {
		deltaUS = timestampUS - c.previousUS
	} else if wrapPeriodUS > c.previousUS {
		deltaUS = wrapPeriodUS - c.previousUS + timestampUS
	}

	c.previousUS = timestampUS
	c.elapsedUS += deltaUS
	return c.elapsedUS, deltaUS
}

func toomossTimestampUS(high byte, low uint32, tickUS uint64) uint64 {
	ticks := uint64(high)<<32 | uint64(low)
	return ticks * tickUS
}

func toomossTimestampWrapUS(tickUS uint64) uint64 {
	return (uint64(1) << 40) * tickUS
}
