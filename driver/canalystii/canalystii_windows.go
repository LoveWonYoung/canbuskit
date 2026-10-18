//go:build windows

// Package canalystii provides the Canalyst-II classic CAN backend.
package canalystii

import (
	"github.com/LoveWonYoung/canbuskit/v2/can"
	"github.com/LoveWonYoung/canbuskit/v2/driver/internal/backend"
)

// USBCAN2 is the ControlCAN device type for CANalyst-II / USBCAN2.
const USBCAN2 = 4

type Options struct {
	DeviceType  int
	DeviceIndex int
}

func New(cfg can.Config, options Options) can.Driver {
	deviceType := options.DeviceType
	if deviceType == 0 {
		deviceType = USBCAN2
	}
	return backend.CanalystIIBus(cfg, deviceType, options.DeviceIndex)
}
