package tp_layer

import "time"

// Config defines the configuration for the ISO-TP Transport.
type Config struct {
	// PaddingByte, if not nil, is used to pad frames to declared length (8 or 64).
	PaddingByte *byte

	TimeoutN_Bs time.Duration // Time until reception of FlowControl
	TimeoutN_Cr time.Duration // Time until reception of next CF

	BlockSize int
	StMin     int
}

// DefaultConfig returns the Lite ISO-TP defaults.
func DefaultConfig() Config {
	return Config{
		PaddingByte: nil, // No padding by default
		TimeoutN_Bs: 1000 * time.Millisecond,
		TimeoutN_Cr: 1000 * time.Millisecond,
		BlockSize:   0,  // BlockSize 0 means unlimited
		StMin:       20, // 20ms separation time
	}
}
