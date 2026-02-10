package driver

import (
	"context"
	"sync"
)

type rxFanout struct {
	mu     sync.RWMutex
	subs   map[chan UnifiedCANMessage]struct{}
	closed bool
	wg     sync.WaitGroup
}

func newRxFanout(ctx context.Context, source <-chan UnifiedCANMessage) *rxFanout {
	f := &rxFanout{
		subs: make(map[chan UnifiedCANMessage]struct{}),
	}
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		for {
			select {
			case <-ctx.Done():
				f.closeAll()
				return
			case msg, ok := <-source:
				if !ok {
					f.closeAll()
					return
				}
				f.dispatch(msg)
			}
		}
	}()
	return f
}

func (f *rxFanout) Subscribe(buffer int) <-chan UnifiedCANMessage {
	ch := make(chan UnifiedCANMessage, buffer)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		close(ch)
		return ch
	}
	f.subs[ch] = struct{}{}
	return ch
}

func (f *rxFanout) dispatch(msg UnifiedCANMessage) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	for ch := range f.subs {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (f *rxFanout) closeAll() {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return
	}
	f.closed = true
	subs := f.subs
	f.subs = nil
	f.mu.Unlock()

	for ch := range subs {
		close(ch)
	}
}

func (f *rxFanout) Close() {
	f.closeAll()
	f.wg.Wait()
}
