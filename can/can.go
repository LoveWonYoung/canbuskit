// Package can defines the platform-independent CAN bus API.
package can

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// Mode selects classic CAN or CAN FD.
type Mode uint8

const (
	Classic Mode = iota
	FD
)

const (
	Channel1 byte = iota
	Channel2
	Channel3
	Channel4
)

// Direction identifies whether a frame was transmitted or received.
type Direction uint8

const (
	Transmit Direction = iota
	Receive
)

// BitTiming is an optional segment set for one CAN bit-rate phase.
// The zero value tells a backend to derive timing from the bitrate.
type BitTiming struct {
	BRP  byte
	SEG1 byte
	SEG2 byte
	SJW  byte
}

// IsZero reports whether the backend should derive this phase from the bitrate.
func (t BitTiming) IsZero() bool { return t == BitTiming{} }

func (t BitTiming) validate() error {
	if t.IsZero() {
		return nil
	}
	if t.BRP == 0 || t.SEG1 == 0 || t.SEG2 == 0 || t.SJW == 0 {
		return errors.New("BRP, SEG1, SEG2, and SJW must all be non-zero")
	}
	return nil
}

// Config contains settings shared by hardware and in-memory backends.
type Config struct {
	Mode            Mode
	Channel         byte
	NominalBitrate  uint32
	DataBitrate     uint32
	RxBufferSize    int
	PollingInterval time.Duration
	IncludeTxEcho   bool
	BRS             bool
	// NominalTiming and DataTiming optionally replace bitrate-derived CAN FD timing.
	// Both phases must be set together. Classic mode does not accept explicit timing.
	NominalTiming BitTiming
	DataTiming    BitTiming
	// Logger receives backend diagnostics and frame logs. Nil disables logging.
	Logger *slog.Logger
}

// DefaultConfig returns a 500 kbit/s arbitration and 2 Mbit/s data setup.
func DefaultConfig(mode Mode, channel byte) Config {
	return Config{
		Mode:            mode,
		Channel:         channel,
		NominalBitrate:  500_000,
		DataBitrate:     2_000_000,
		RxBufferSize:    1024,
		PollingInterval: time.Millisecond,
	}
}

// Validate reports invalid configuration values. Zero numeric values select
// defaults where documented by Normalized.
func (cfg Config) Validate() error {
	_, err := cfg.Normalized()
	return err
}

// Normalized validates cfg and fills optional zero values.
func (cfg Config) Normalized() (Config, error) {
	switch cfg.Mode {
	case Classic, FD:
	default:
		return Config{}, fmt.Errorf("unsupported CAN mode: %d", cfg.Mode)
	}
	if cfg.Mode == Classic && cfg.BRS {
		return Config{}, errors.New("BRS requires CAN FD mode")
	}
	if err := cfg.NominalTiming.validate(); err != nil {
		return Config{}, fmt.Errorf("nominal timing: %w", err)
	}
	if err := cfg.DataTiming.validate(); err != nil {
		return Config{}, fmt.Errorf("data timing: %w", err)
	}
	if cfg.Mode == Classic && (!cfg.NominalTiming.IsZero() || !cfg.DataTiming.IsZero()) {
		return Config{}, errors.New("explicit bit timing requires CAN FD mode")
	}
	if cfg.Mode == FD && cfg.NominalTiming.IsZero() != cfg.DataTiming.IsZero() {
		return Config{}, errors.New("CAN FD explicit timing requires both nominal and data phases")
	}
	if cfg.NominalBitrate == 0 {
		cfg.NominalBitrate = 500_000
	}
	if cfg.DataBitrate == 0 {
		cfg.DataBitrate = 2_000_000
	}
	if cfg.RxBufferSize == 0 {
		cfg.RxBufferSize = 1024
	}
	if cfg.RxBufferSize < 0 {
		return Config{}, fmt.Errorf("receive buffer size must be >= 0: %d", cfg.RxBufferSize)
	}
	if cfg.PollingInterval == 0 {
		cfg.PollingInterval = time.Millisecond
	}
	if cfg.PollingInterval < 0 {
		return Config{}, fmt.Errorf("polling interval must be >= 0: %s", cfg.PollingInterval)
	}
	return cfg, nil
}

// Frame is the common representation of a classic CAN or CAN FD frame.
type Frame struct {
	Direction   Direction
	ID          uint32
	DLC         byte
	Data        [64]byte
	FD          bool
	BRS         bool
	Extended    bool
	TimestampUS uint64
}

// FrameFlags describes wire-format features without positional booleans.
type FrameFlags struct {
	FD       bool
	BRS      bool
	Extended bool
}

// NewFrame validates and copies payload into a frame value.
func NewFrame(id uint32, payload []byte, flags FrameFlags) (Frame, error) {
	frame := Frame{ID: id, FD: flags.FD, BRS: flags.BRS, Extended: flags.Extended}
	if err := validateFrame(frame, len(payload)); err != nil {
		return Frame{}, err
	}
	frame.DLC = LengthToDLC(len(payload))
	copy(frame.Data[:], payload)
	return frame, nil
}

func validateFrame(frame Frame, payloadLength int) error {
	maxID := uint32(0x7FF)
	if frame.Extended {
		maxID = 0x1FFFFFFF
	}
	if frame.ID > maxID {
		return fmt.Errorf("CAN ID 0x%X exceeds maximum 0x%X", frame.ID, maxID)
	}
	if payloadLength < 0 {
		return errors.New("CAN payload length cannot be negative")
	}
	maximum := 8
	if frame.FD {
		maximum = 64
	} else if frame.BRS {
		return errors.New("BRS requires a CAN FD frame")
	}
	if payloadLength > maximum {
		return fmt.Errorf("CAN payload length %d exceeds maximum %d", payloadLength, maximum)
	}
	return nil
}

// Validate checks the frame ID, DLC, and flag combination.
func (f Frame) Validate() error {
	length, ok := f.ValidDataLength()
	if !ok {
		return fmt.Errorf("invalid DLC %d for frame format", f.DLC)
	}
	return validateFrame(f, length)
}

// LengthToDLC converts a payload length to the smallest matching CAN DLC.
func LengthToDLC(length int) byte {
	if length <= 8 {
		if length < 0 {
			return 0
		}
		return byte(length)
	}
	switch {
	case length <= 12:
		return 9
	case length <= 16:
		return 10
	case length <= 20:
		return 11
	case length <= 24:
		return 12
	case length <= 32:
		return 13
	case length <= 48:
		return 14
	default:
		return 15
	}
}

// DLCToLength converts a valid CAN DLC into its represented payload length.
func DLCToLength(dlc byte) (int, bool) {
	if dlc <= 8 {
		return int(dlc), true
	}
	lengths := [...]int{12, 16, 20, 24, 32, 48, 64}
	if dlc > 15 {
		return 0, false
	}
	return lengths[dlc-9], true
}

// DataLength returns the payload length represented by DLC.
func (f Frame) DataLength() int {
	length, _ := DLCToLength(f.DLC)
	return length
}

// ValidDataLength returns the payload length and whether DLC matches the frame format.
func (f Frame) ValidDataLength() (int, bool) {
	if (!f.FD && f.DLC > 8) || (f.FD && f.DLC > 15) {
		return 0, false
	}
	return DLCToLength(f.DLC)
}

// Payload returns a copy of the bytes represented by DLC.
func (f Frame) Payload() []byte {
	length, ok := f.ValidDataLength()
	if !ok {
		return nil
	}
	payload := make([]byte, length)
	copy(payload, f.Data[:length])
	return payload
}

// Stats is a snapshot of receive-side queue pressure.
type Stats struct {
	SourceDropped     uint64
	SubscriberDropped uint64
}

// BusLoad is a snapshot of estimated bus occupancy over a recent window.
type BusLoad struct {
	Load           float64
	Window         time.Duration
	NominalBitrate uint32
	DataBitrate    uint32
	FrameCount     uint64
}

var (
	ErrNotOpen     = errors.New("CAN bus is not open")
	ErrAlreadyOpen = errors.New("CAN bus is already open")
	ErrUnsupported = errors.New("CAN operation is not supported")
	ErrRxOverflow  = errors.New("CAN receive queue overflow")
)

// ReceiveOverflowError identifies where a CAN frame was dropped.
type ReceiveOverflowError struct {
	Stage   string
	Dropped uint64
}

func (e *ReceiveOverflowError) Error() string {
	return fmt.Sprintf("%s: stage=%s dropped=%d", ErrRxOverflow, e.Stage, e.Dropped)
}

func (e *ReceiveOverflowError) Unwrap() error { return ErrRxOverflow }

// Driver is the complete lifecycle and I/O contract implemented by backends.
// The caller owns the driver: Open it before use and Close it after all clients
// have stopped. Errors returns one channel for the life of the driver; Close
// does not close or replace it. A later Open delivers subsequent errors on
// that same channel.
type Driver interface {
	Open(context.Context) error
	Close() error
	Send(context.Context, Frame) error
	Subscribe(buffer int) (<-chan Frame, func(), error)
	Errors() <-chan error
	Stats() Stats
	BusLoad() BusLoad
	Config() Config
}
