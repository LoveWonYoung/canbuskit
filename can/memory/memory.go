// Package memory provides an in-process CAN link for tests and simulations.
package memory

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/LoveWonYoung/canbuskit/v2/can"
)

// Pair returns two endpoints connected back-to-back. A frame sent through one
// endpoint is received by every subscriber of the other endpoint.
func Pair(cfg can.Config) (can.Driver, can.Driver) {
	a := newEndpoint(cfg)
	b := newEndpoint(cfg)
	a.peer = b
	b.peer = a
	return a, b
}

type endpoint struct {
	cfg can.Config

	mu          sync.RWMutex
	peer        *endpoint
	opened      bool
	closed      bool
	subscribers map[chan can.Frame]struct{}
	errors      chan error
	dropped     atomic.Uint64
	frames      atomic.Uint64
}

func newEndpoint(cfg can.Config) *endpoint {
	if cfg.RxBufferSize == 0 {
		cfg.RxBufferSize = 1024
	}
	return &endpoint{
		cfg:         cfg,
		subscribers: make(map[chan can.Frame]struct{}),
		errors:      make(chan error, 16),
	}
}

func (e *endpoint) Open(ctx context.Context) error {
	if ctx == nil {
		return errors.New("open context cannot be nil")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return errors.New("memory CAN endpoint is closed")
	}
	if e.opened {
		return can.ErrAlreadyOpen
	}
	normalized, err := e.cfg.Normalized()
	if err != nil {
		return err
	}
	e.cfg = normalized
	e.opened = true
	return nil
}

func (e *endpoint) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.opened = false
	e.closed = true
	for ch := range e.subscribers {
		close(ch)
	}
	e.subscribers = nil
	close(e.errors)
	e.mu.Unlock()
	return nil
}

func (e *endpoint) Send(ctx context.Context, frame can.Frame) error {
	if ctx == nil {
		return errors.New("send context cannot be nil")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := frame.Validate(); err != nil {
		return err
	}
	e.mu.RLock()
	opened := e.opened && !e.closed
	peer := e.peer
	includeEcho := e.cfg.IncludeTxEcho
	mode := e.cfg.Mode
	e.mu.RUnlock()
	if !opened {
		return can.ErrNotOpen
	}
	if mode == can.Classic && frame.FD {
		return errors.New("CAN FD frame cannot be sent in classic CAN mode")
	}
	if peer == nil {
		return errors.New("memory CAN endpoint is not connected")
	}

	received := frame
	received.Direction = can.Receive
	if err := peer.dispatch(ctx, received); err != nil {
		return err
	}
	e.frames.Add(1)
	if includeEcho {
		echo := frame
		echo.Direction = can.Transmit
		_ = e.dispatch(ctx, echo)
	}
	return nil
}

func (e *endpoint) dispatch(ctx context.Context, frame can.Frame) error {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if !e.opened || e.closed {
		return can.ErrNotOpen
	}
	for ch := range e.subscribers {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ch <- frame:
		default:
			e.dropped.Add(1)
		}
	}
	return nil
}

func (e *endpoint) Subscribe(buffer int) (<-chan can.Frame, func(), error) {
	if buffer < 0 {
		buffer = 0
	}
	ch := make(chan can.Frame, buffer)
	e.mu.Lock()
	if !e.opened || e.closed {
		close(ch)
		e.mu.Unlock()
		return ch, func() {}, can.ErrNotOpen
	}
	e.subscribers[ch] = struct{}{}
	e.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			e.mu.Lock()
			if _, exists := e.subscribers[ch]; exists {
				delete(e.subscribers, ch)
				close(ch)
			}
			e.mu.Unlock()
		})
	}, nil
}

func (e *endpoint) Errors() <-chan error { return e.errors }

func (e *endpoint) Stats() can.Stats {
	return can.Stats{SubscriberDropped: e.dropped.Load()}
}

func (e *endpoint) BusLoad() can.BusLoad {
	e.mu.RLock()
	cfg := e.cfg
	e.mu.RUnlock()
	return can.BusLoad{
		NominalBitrate: cfg.NominalBitrate,
		DataBitrate:    cfg.DataBitrate,
		FrameCount:     e.frames.Load(),
	}
}

func (e *endpoint) Config() can.Config {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.cfg
}
