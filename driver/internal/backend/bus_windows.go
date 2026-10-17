//go:build windows

package backend

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/LoveWonYoung/canbuskit/v2/can"
)

// bus owns the public lifecycle around a low-level vendor device.
type bus struct {
	device device
	mu     sync.RWMutex
	sendMu sync.Mutex
	open   bool
}

func newBus(device device) can.Driver {
	return &bus{device: device}
}

func (b *bus) Open(ctx context.Context) error {
	if ctx == nil {
		return errors.New("open context cannot be nil")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.open {
		return can.ErrAlreadyOpen
	}
	if err := b.device.initialize(); err != nil {
		b.device.stop()
		return fmt.Errorf("initialize CAN backend: %w", err)
	}
	if err := b.device.start(); err != nil {
		b.device.stop()
		return fmt.Errorf("start CAN backend: %w", err)
	}
	b.open = true
	return nil
}

func (b *bus) Close() error {
	b.sendMu.Lock()
	defer b.sendMu.Unlock()
	b.mu.Lock()
	if !b.open {
		b.mu.Unlock()
		return nil
	}
	b.open = false
	b.mu.Unlock()
	b.device.stop()
	return nil
}

func (b *bus) Send(ctx context.Context, frame can.Frame) error {
	if ctx == nil {
		return errors.New("send context cannot be nil")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if frame.Extended {
		return fmt.Errorf("%w: 29-bit CAN IDs", can.ErrUnsupported)
	}
	length, ok := frame.ValidDataLength()
	if !ok {
		return fmt.Errorf("invalid DLC %d for frame format", frame.DLC)
	}
	if err := frame.Validate(); err != nil {
		return err
	}

	b.sendMu.Lock()
	defer b.sendMu.Unlock()
	b.mu.RLock()
	opened := b.open
	b.mu.RUnlock()
	if !opened {
		return can.ErrNotOpen
	}
	b.device.setBRS(frame.BRS)
	if err := b.device.write(int32(frame.ID), frame.FD, frame.Data[:length]); err != nil {
		return fmt.Errorf("send CAN frame 0x%X: %w", frame.ID, err)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (b *bus) Subscribe(buffer int) (<-chan can.Frame, func(), error) {
	b.mu.RLock()
	opened := b.open
	b.mu.RUnlock()
	if !opened {
		ch := make(chan can.Frame)
		close(ch)
		return ch, func() {}, can.ErrNotOpen
	}
	frames, unsubscribe := b.device.subscribe(buffer)
	return frames, unsubscribe, nil
}

func (b *bus) Errors() <-chan error {
	return b.device.errors()
}

func (b *bus) Stats() can.Stats {
	return b.device.stats()
}

func (b *bus) BusLoad() can.BusLoad {
	return b.device.busLoad()
}

func (b *bus) config() can.Config {
	return b.device.config()
}

func Auto(cfg can.Config) can.Driver {
	return newBus(newAutoDevice(cfg))
}

func ToomossBus(cfg can.Config) can.Driver {
	return newBus(NewToomossWithConfig(cfg))
}

func PCANBus(cfg can.Config) can.Driver {
	return newBus(NewPCANWithConfig(cfg))
}

func VectorBus(cfg can.Config, deviceType int) can.Driver {
	return newBus(NewVectorWithConfig(cfg, deviceType))
}

func CanalystIIBus(cfg can.Config, deviceType, deviceIndex int) can.Driver {
	return newBus(NewCanalystIIWithDevice(cfg, deviceType, deviceIndex))
}

func TSMasterBus(cfg can.Config, deviceType int, mapping *TSMasterMapping) can.Driver {
	if mapping == nil {
		return newBus(NewTSMasterWithConfig(cfg, deviceType))
	}
	return newBus(NewTSMasterWithMapping(cfg, deviceType, *mapping))
}
