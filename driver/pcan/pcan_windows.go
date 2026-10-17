//go:build windows

// Package pcan provides the PEAK-System PCAN backend.
package pcan

import (
	"github.com/LoveWonYoung/canbuskit/v2/can"
	"github.com/LoveWonYoung/canbuskit/v2/driver/internal/adapter"
	legacy "github.com/LoveWonYoung/canbuskit/v2/internal/windriver"
)

func New(cfg can.Config) can.Driver {
	return adapter.Wrap(legacy.NewPCANWithConfig(cfg))
}
