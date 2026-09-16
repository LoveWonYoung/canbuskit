package tp_layer

import (
	"fmt"
	"time"
)

const (
	defaultMaxPayloadSize = 16 * 1024 * 1024
	defaultMaxWaitFrames  = 8
)

// Config defines the configuration for the ISO-TP Transport.
type Config struct {
	// PaddingByte, if not nil, is used to pad frames to declared length (8 or 64).
	PaddingByte *byte

	TimeoutN_Bs time.Duration // Time until reception of FlowControl
	TimeoutN_Cr time.Duration // Time until reception of next CF

	BlockSize int
	StMin     int

	// MaxPayloadSize limits a reassembled or queued ISO-TP payload. Zero uses
	// the safe default (16 MiB). This prevents a malformed 32-bit First Frame
	// length from causing an unbounded allocation.
	MaxPayloadSize int
	// MaxWaitFrames limits consecutive FlowControl/WAIT frames. Zero uses the
	// default of 8. A negative value disables the limit.
	MaxWaitFrames int
}

// DefaultConfig returns the Lite ISO-TP defaults.
func DefaultConfig() Config {
	return Config{
		PaddingByte:    nil, // No padding by default
		TimeoutN_Bs:    1000 * time.Millisecond,
		TimeoutN_Cr:    1000 * time.Millisecond,
		BlockSize:      0,  // BlockSize 0 means unlimited
		StMin:          20, // 20ms separation time
		MaxPayloadSize: defaultMaxPayloadSize,
		MaxWaitFrames:  defaultMaxWaitFrames,
	}
}

// Validate reports invalid ISO-TP settings. Zero values for MaxPayloadSize and
// MaxWaitFrames select safe defaults for backwards compatibility.
func (cfg Config) Validate() error {
	if cfg.TimeoutN_Bs <= 0 {
		return fmt.Errorf("TimeoutN_Bs must be greater than zero: %s", cfg.TimeoutN_Bs)
	}
	if cfg.TimeoutN_Cr <= 0 {
		return fmt.Errorf("TimeoutN_Cr must be greater than zero: %s", cfg.TimeoutN_Cr)
	}
	if cfg.BlockSize < 0 || cfg.BlockSize > 255 {
		return fmt.Errorf("block size must be between 0 and 255: %d", cfg.BlockSize)
	}
	if cfg.StMin < 0 || cfg.StMin > 127 {
		return fmt.Errorf("STmin must be between 0 and 127 milliseconds: %d", cfg.StMin)
	}
	if cfg.MaxPayloadSize < 0 {
		return fmt.Errorf("maximum payload size must be >= 0: %d", cfg.MaxPayloadSize)
	}
	return nil
}

func normalizeConfig(cfg Config) (Config, error) {
	if cfg.PaddingByte != nil {
		padding := *cfg.PaddingByte
		cfg.PaddingByte = &padding
	}
	if cfg.MaxPayloadSize == 0 {
		cfg.MaxPayloadSize = defaultMaxPayloadSize
	}
	if cfg.MaxWaitFrames == 0 {
		cfg.MaxWaitFrames = defaultMaxWaitFrames
	}
	return cfg, cfg.Validate()
}
