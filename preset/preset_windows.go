//go:build windows

package preset

import (
	"fmt"

	"github.com/LoveWonYoung/canbuskit/driver"
)

func NewPresetTSMaster(physId, respId, funcId uint32, channel byte, canType driver.CanType, deviceType int) (*Preset, error) {
	drv := driver.NewTSMaster(canType, channel, deviceType)
	return newPreset(drv, physId, respId, funcId)
}

func NewPresetPCAN(physId, respId, funcId uint32, channel byte, canType driver.CanType) (*Preset, error) {
	drv := driver.NewPCAN(canType, channel)
	return newPreset(drv, physId, respId, funcId)
}

func NewPresetPCANWithCANFDConfig(
	physId, respId, funcId uint32,
	channel byte,
	canType driver.CanType,
	cfg driver.CANFDInitConfig) (*Preset, error) {
	if err := cfg.ValidateWithBRP(); err != nil {
		return nil, fmt.Errorf("invalid CAN FD config: %w", err)
	}
	drv := driver.NewPCAN(canType, channel)
	drv.SetCANFDInitConfig(cfg)
	return newPreset(drv, physId, respId, funcId)
}

func NewPresetVector(physId, respId, funcId uint32, channel byte, canType driver.CanType, deviceType int) (*Preset, error) {
	drv := driver.NewVector(canType, deviceType, int(channel))
	return newPreset(drv, physId, respId, funcId)
}

func NewPresetVectorWithCANFDConfig(
	physId, respId, funcId uint32,
	channel byte,
	canType driver.CanType,
	deviceType int,
	cfg driver.CANFDInitConfig) (*Preset, error) {
	if err := cfg.ValidateTiming(); err != nil {
		return nil, fmt.Errorf("invalid CAN FD config: %w", err)
	}
	drv := driver.NewVector(canType, deviceType, int(channel))
	drv.SetCANFDInitConfig(cfg)
	return newPreset(drv, physId, respId, funcId)
}
