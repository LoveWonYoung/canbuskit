//go:build windows

// Package tsmaster provides the TOSUN TSMaster backend.
package tsmaster

import (
	"github.com/LoveWonYoung/canbuskit/v2/can"
	"github.com/LoveWonYoung/canbuskit/v2/driver/internal/adapter"
	legacy "github.com/LoveWonYoung/canbuskit/v2/internal/windriver"
)

type Options struct {
	DeviceType int
	Mapping    *Mapping
}

// Mapping connects one application channel to one physical hardware channel.
type Mapping struct {
	ApplicationChannel byte
	HardwareIndex      int
	HardwareChannel    byte
}

func New(cfg can.Config, options Options) can.Driver {
	deviceType := options.DeviceType
	if deviceType == 0 {
		deviceType = legacy.TC1016
	}
	if options.Mapping == nil {
		return adapter.Wrap(legacy.NewTSMasterWithConfig(cfg, deviceType))
	}
	mapping := legacy.TSMasterMapping{
		ApplicationChannel: options.Mapping.ApplicationChannel,
		HardwareIndex:      options.Mapping.HardwareIndex,
		HardwareChannel:    options.Mapping.HardwareChannel,
	}
	return adapter.Wrap(legacy.NewTSMasterWithMapping(cfg, deviceType, mapping))
}
