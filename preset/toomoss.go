//go:build windows || (darwin && cgo)

package preset

import (
	"fmt"

	"github.com/LoveWonYoung/canbuskit/driver"
)

func NewPresetToomoss(physId, respId, funcId uint32, channel byte, canType driver.CanType) (*Preset, error) {
	drv := driver.NewToomoss(canType, channel)
	return newPreset(drv, physId, respId, funcId)
}

func NewPresetToomossWithCANFDConfig(
	physId, respId, funcId uint32,
	channel byte,
	canType driver.CanType,
	cfg driver.CANFDInitConfig) (*Preset, error) {
	if err := cfg.ValidateWithBRP(); err != nil {
		return nil, fmt.Errorf("invalid CAN FD config: %w", err)
	}
	drv := driver.NewToomoss(canType, channel)
	drv.SetCANFDTiming(cfg)
	return newPreset(drv, physId, respId, funcId)
}
