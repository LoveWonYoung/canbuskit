//go:build windows

// Package toomoss provides the Toomoss USB2CAN backend.
package toomoss

import (
	"github.com/LoveWonYoung/canbuskit/v2/can"
	"github.com/LoveWonYoung/canbuskit/v2/driver/internal/adapter"
	legacy "github.com/LoveWonYoung/canbuskit/v2/internal/windriver"
)

func New(cfg can.Config) can.Driver {
	return adapter.Wrap(legacy.NewToomossWithConfig(cfg))
}
