//go:build !windows

package preset

import (
	"errors"

	"github.com/LoveWonYoung/canbuskit/driver"
)

func NewPresetAuto(physId, respId, funcId uint32, canType driver.CanType) (*Preset, error) {
	return nil, errors.New("preset: NewPresetAuto is only supported on Windows")
}
