//go:build windows || (darwin && cgo)

package preset

import "github.com/LoveWonYoung/canbuskit/driver"

func NewPresetToomoss(physId, respId, funcId uint32, channel byte, canType driver.CanType, options ...Option) (*Preset, error) {
	drv := driver.NewToomoss(canType, channel)
	return newPreset(drv, physId, respId, funcId, options...)
}
