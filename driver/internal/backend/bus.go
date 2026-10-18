package backend

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/LoveWonYoung/canbuskit/v2/can"
)

// bus owns the public lifecycle around a low-level vendor device.
// Lock order is sendMu, then mu, then the device's own lock.
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
	if err := b.device.write(frame); err != nil {
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
	defer b.mu.RUnlock()
	if !b.open {
		ch := make(chan can.Frame)
		close(ch)
		return ch, func() {}, can.ErrNotOpen
	}
	frames, unsubscribe := b.device.subscribe(buffer)
	if frames == nil {
		ch := make(chan can.Frame)
		close(ch)
		return ch, func() {}, can.ErrNotOpen
	}
	if unsubscribe == nil {
		unsubscribe = func() {}
	}
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

func (b *bus) Config() can.Config {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.device.config()
}
