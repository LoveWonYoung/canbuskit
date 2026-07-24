//go:build windows

package preset

import "github.com/LoveWonYoung/canbuskit/driver"

func NewPresetTSMaster(physId, respId, funcId uint32, channel byte, canType driver.CanType, deviceType int) (*Preset, error) {
	drv := driver.NewTSMaster(canType, channel, deviceType)
	return newPreset(drv, physId, respId, funcId, canType == driver.CANFD)
}

func NewPresetPCAN(physId, respId, funcId uint32, channel byte, canType driver.CanType) (*Preset, error) {
	drv := driver.NewPCAN(canType, channel)
	return newPreset(drv, physId, respId, funcId, canType == driver.CANFD)
}

func NewPresetVector(physId, respId, funcId uint32, channel byte, canType driver.CanType, deviceType int) (*Preset, error) {
	drv := driver.NewVector(canType, deviceType, int(channel))
	return newPreset(drv, physId, respId, funcId, canType == driver.CANFD)
}
