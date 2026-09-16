//go:build windows

package preset

import "github.com/LoveWonYoung/canbuskit/driver"

func NewPresetAuto(physId, respId, funcId uint32, canType driver.CanType, options ...Option) (*Preset, error) {
	return newPreset(driver.NewAutoDriver(canType), physId, respId, funcId, options...)
}
