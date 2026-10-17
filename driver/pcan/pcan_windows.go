//go:build windows

// Package pcan provides the PEAK-System PCAN backend.
package pcan

import (
	"github.com/LoveWonYoung/canbuskit/v2/can"
	"github.com/LoveWonYoung/canbuskit/v2/driver/internal/backend"
)

func New(cfg can.Config) can.Driver {
	return backend.PCANBus(cfg)
}
