//go:build windows

// Package vector provides the Vector XL Driver Library backend.
package vector

import (
	"github.com/LoveWonYoung/canbuskit/v2/can"
	"github.com/LoveWonYoung/canbuskit/v2/driver/internal/adapter"
	legacy "github.com/LoveWonYoung/canbuskit/v2/internal/windriver"
)

const (
	VN1630 = legacy.CANOEVN1630
	VN1640 = legacy.CANOEVN1640
)

type Options struct {
	DeviceType int
}

func New(cfg can.Config, options Options) can.Driver {
	deviceType := options.DeviceType
	if deviceType == 0 {
		deviceType = VN1640
	}
	return adapter.Wrap(legacy.NewVectorWithConfig(cfg, deviceType))
}
