//go:build windows

// Package canalystii provides the Canalyst-II classic CAN backend.
package canalystii

import (
	"github.com/LoveWonYoung/canbuskit/v2/can"
	"github.com/LoveWonYoung/canbuskit/v2/driver/internal/adapter"
	legacy "github.com/LoveWonYoung/canbuskit/v2/internal/windriver"
)

type Options struct {
	DeviceType  int
	DeviceIndex int
}

func New(cfg can.Config, options Options) can.Driver {
	if options.DeviceType == 0 {
		options.DeviceType = 4 // VCI_USBCAN2
	}
	return adapter.Wrap(legacy.NewCanalystIIWithDevice(cfg, options.DeviceType, options.DeviceIndex))
}
