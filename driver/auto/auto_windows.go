//go:build windows

// Package auto probes supported Windows CAN backends in priority order.
package auto

import (
	"github.com/LoveWonYoung/canbuskit/v2/can"
	"github.com/LoveWonYoung/canbuskit/v2/driver/internal/backend"
)

func New(cfg can.Config) can.Driver {
	return backend.Auto(cfg)
}
