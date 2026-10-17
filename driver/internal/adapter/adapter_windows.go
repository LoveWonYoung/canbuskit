//go:build windows

package adapter

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/LoveWonYoung/canbuskit/v2/can"
	legacy "github.com/LoveWonYoung/canbuskit/v2/internal/windriver"
)

// Bus adapts the private vendor implementation to the public lifecycle API.
type Bus struct {
	device legacy.CANDriver
	mu     sync.RWMutex
	sendMu sync.Mutex
	open   bool
}

func Wrap(device legacy.CANDriver) can.Driver {
	return &Bus{device: device}
}

func (b *Bus) Open(ctx context.Context) error {
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
	if err := b.device.Init(); err != nil {
		b.device.Stop()
		return fmt.Errorf("initialize CAN backend: %w", err)
	}
	if starter, ok := b.device.(legacy.ErrorStartingCANDriver); ok {
		if err := starter.StartWithError(); err != nil {
			b.device.Stop()
			return fmt.Errorf("start CAN backend: %w", err)
		}
	} else {
		b.device.Start()
	}
	b.open = true
	return nil
}

func (b *Bus) Close() error {
	b.sendMu.Lock()
	defer b.sendMu.Unlock()
	b.mu.Lock()
	if !b.open {
		b.mu.Unlock()
		return nil
	}
	b.open = false
	b.mu.Unlock()
	b.device.Stop()
	return nil
}

func (b *Bus) Send(ctx context.Context, frame can.Frame) error {
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
	if controller, ok := b.device.(legacy.BRSController); ok {
		controller.SetBRS(frame.BRS)
	} else if frame.BRS {
		return fmt.Errorf("%w: bit-rate switching", can.ErrUnsupported)
	}
	if err := b.device.Write(int32(frame.ID), frame.FD, frame.Data[:length]); err != nil {
		return fmt.Errorf("send CAN frame 0x%X: %w", frame.ID, err)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (b *Bus) Subscribe(buffer int) (<-chan can.Frame, func(), error) {
	b.mu.RLock()
	opened := b.open
	b.mu.RUnlock()
	if !opened {
		ch := make(chan can.Frame)
		close(ch)
		return ch, func() {}, can.ErrNotOpen
	}
	if subscriber, ok := b.device.(legacy.RxSubscriber); ok {
		frames, unsubscribe := subscriber.SubscribeRx(buffer)
		return frames, unsubscribe, nil
	}
	return b.device.RxChan(), func() {}, nil
}

func (b *Bus) Errors() <-chan error {
	if observable, ok := b.device.(legacy.ObservableCANDriver); ok {
		return observable.Errors()
	}
	ch := make(chan error)
	close(ch)
	return ch
}

func (b *Bus) Stats() can.Stats {
	if observable, ok := b.device.(legacy.ObservableCANDriver); ok {
		return observable.Stats()
	}
	return can.Stats{}
}

func (b *Bus) BusLoad() can.BusLoad {
	if observable, ok := b.device.(legacy.ObservableCANDriver); ok {
		return observable.BusLoad()
	}
	return can.BusLoad{}
}

func (b *Bus) Config() can.Config {
	if configured, ok := b.device.(interface{ Config() legacy.Config }); ok {
		return configured.Config()
	}
	return can.Config{}
}
