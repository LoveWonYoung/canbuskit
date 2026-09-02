package driver

import (
	"fmt"
	"sync/atomic"
	"time"
)

var printLog atomic.Bool

// LogFilterMode controls which CAN messages are written to the log.
type LogFilterMode uint8

const (
	// LogFilterOff disables log filtering and allows every CAN ID.
	LogFilterOff LogFilterMode = iota
	// LogFilterList allows only CAN IDs configured by SetLogFilter.
	LogFilterList
)

type compiledLogFilter struct {
	ids [32]uint64 // 2048 bits for standard 11-bit CAN IDs.
}

func (f *compiledLogFilter) allows(id uint32) bool {
	if id > 0x7FF {
		return false
	}
	return f.ids[id>>6]&(uint64(1)<<(id&63)) != 0
}

var logFilter atomic.Pointer[compiledLogFilter]

// Config contains settings shared by all CAN hardware backends.
//
// The driver package intentionally supports standard (11-bit) CAN and CAN-FD
// frames only. Vendor-specific device selection remains on the corresponding
// constructor (for example Vector deviceType).
type Config struct {
	Mode            CanType
	Channel         byte // Physical hardware channel, zero-based.
	NominalBitrate  uint32
	DataBitrate     uint32
	RxBufferSize    int
	PollingInterval time.Duration
	IncludeTxEcho   bool
	// BRS enables CAN-FD bit rate switching on transmitted FD frames.
	// Classic CAN frames are unaffected. Default is off.
	BRS bool
}

// DefaultConfig returns the backwards-compatible 500 kbit/s / 2 Mbit/s setup.
func DefaultConfig(mode CanType, channel byte) Config {
	return Config{
		Mode:            mode,
		Channel:         channel,
		NominalBitrate:  500_000,
		DataBitrate:     2_000_000,
		RxBufferSize:    RxChannelBufferSize,
		PollingInterval: PollingInterval,
	}
}

// Validate checks whether the shared configuration values are usable. A zero
// bitrate, buffer size, or polling interval selects its default value.
func (cfg Config) Validate() error {
	_, err := normalizeConfig(cfg)
	return err
}

func normalizeConfig(cfg Config) (Config, error) {
	switch cfg.Mode {
	case CAN, CANFD:
	default:
		return Config{}, fmt.Errorf("unsupported CAN mode: %d", cfg.Mode)
	}
	if cfg.NominalBitrate == 0 {
		cfg.NominalBitrate = 500_000
	}
	if cfg.DataBitrate == 0 {
		cfg.DataBitrate = 2_000_000
	}
	if cfg.RxBufferSize == 0 {
		cfg.RxBufferSize = RxChannelBufferSize
	}
	if cfg.RxBufferSize < 0 {
		return Config{}, fmt.Errorf("receive buffer size must be >= 0: %d", cfg.RxBufferSize)
	}
	if cfg.PollingInterval == 0 {
		cfg.PollingInterval = PollingInterval
	}
	if cfg.PollingInterval < 0 {
		return Config{}, fmt.Errorf("polling interval must be >= 0: %s", cfg.PollingInterval)
	}
	if cfg.Mode == CANFD && cfg.DataBitrate == 0 {
		return Config{}, fmt.Errorf("CAN-FD data bitrate must be greater than zero")
	}
	return cfg, nil
}

func validateWrite(cfg Config, id int32, fd bool, data []byte) error {
	if id < 0 || id > 0x7FF {
		return fmt.Errorf("standard CAN ID 0x%X out of range (0x000-0x7FF)", id)
	}
	if fd && cfg.Mode != CANFD {
		return fmt.Errorf("driver is configured for classic CAN")
	}
	if !fd && len(data) > 8 {
		return fmt.Errorf("data length %d exceeds CAN maximum of 8", len(data))
	}
	if fd && len(data) > 64 {
		return fmt.Errorf("data length %d exceeds CAN-FD maximum of 64", len(data))
	}
	return nil
}

func SetPrintLog(b bool) {
	printLog.Store(b)
}

func printLogEnabled() bool {
	return printLog.Load()
}

// SetLogFilter configures filtering for CAN message logs. LogFilterOff allows
// every ID. LogFilterList allows only IDs present in list; an empty list
// suppresses all CAN message logs.
func SetLogFilter(mode LogFilterMode, list []uint32) error {
	switch mode {
	case LogFilterOff:
		logFilter.Store(nil)
		return nil
	case LogFilterList:
	default:
		return fmt.Errorf("unsupported log filter mode: %d", mode)
	}

	filter := &compiledLogFilter{}
	for _, id := range list {
		if id > 0x7FF {
			return fmt.Errorf("log filter CAN ID 0x%X out of range (0x000-0x7FF)", id)
		}
		filter.ids[id>>6] |= uint64(1) << (id & 63)
	}
	logFilter.Store(filter)
	return nil
}
