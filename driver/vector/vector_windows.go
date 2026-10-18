//go:build windows

// Package vector provides the Vector XL Driver Library backend.
package vector

import (
	"github.com/LoveWonYoung/canbuskit/v2/can"
	"github.com/LoveWonYoung/canbuskit/v2/driver/internal/backend"
)

const (
	VN1630 = 57
	VN1640 = 59
)

type Options struct {
	DeviceType int
}

func New(cfg can.Config, options Options) can.Driver {
	deviceType := options.DeviceType
	if deviceType == 0 {
		deviceType = VN1640
	}
	return backend.VectorBus(cfg, deviceType)
}
