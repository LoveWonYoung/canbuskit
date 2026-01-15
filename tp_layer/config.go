package tp_layer

import "time"

// Config defines the configuration for the ISO-TP Transport.
type Config struct {
	// PaddingByte, if not nil, is used to pad frames to declared length (8 or 64).
	PaddingByte *byte

	// Transmitter Side Timeouts
	TimeoutN_As time.Duration // Time for transmission of N_PDU on sender side
	TimeoutN_Bs time.Duration // Time until reception of FlowControl
	TimeoutN_Cs time.Duration // Time until transmission of next CF

	// Receiver Side Timeouts
	TimeoutN_Ar time.Duration // Time for transmission of N_PDU on receiver side
	TimeoutN_Br time.Duration // Time until transmission of FlowControl
	TimeoutN_Cr time.Duration // Time until reception of next CF

	// Parameters
	BlockSize int
	StMin     int
}

// DefaultConfig returns the standard ISO-15765-2 default messages.
func DefaultConfig() Config {
	return Config{
		PaddingByte: nil, // No padding by default

		// Default Timeouts (ISO 15765-2 recommended values)
		TimeoutN_As: 1000 * time.Millisecond,
		TimeoutN_Bs: 1000 * time.Millisecond,
		TimeoutN_Cs: 1000 * time.Millisecond,

		TimeoutN_Ar: 1000 * time.Millisecond,
		TimeoutN_Br: 1000 * time.Millisecond,
		TimeoutN_Cr: 1000 * time.Millisecond,

		BlockSize: 0,  // BlockSize 0 means unlimited
		StMin:     20, // 20ms separation time
	}
}
