package driver

import (
	"context"
	"sync"
)

type rxFanout struct {
	mu        sync.RWMutex
	subs      map[chan CanFrame]struct{}
	closed    bool
	wg        sync.WaitGroup
	telemetry *driverTelemetry
}

func newRxFanout(ctx context.Context, source <-chan CanFrame, telemetry *driverTelemetry) *rxFanout {
	f := &rxFanout{
		subs:      make(map[chan CanFrame]struct{}),
		telemetry: telemetry,
	}
	f.wg.Go(func() {
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
	})
	return f
}

func (f *rxFanout) Subscribe(buffer int) (<-chan CanFrame, func()) {
	if buffer < 0 {
		buffer = 0
	}
	ch := make(chan CanFrame, buffer)
	f.mu.Lock()
	if f.closed {
		close(ch)
		f.mu.Unlock()
		return ch, func() {}
	}
	f.subs[ch] = struct{}{}
	f.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			f.mu.Lock()
			if _, ok := f.subs[ch]; ok {
				delete(f.subs, ch)
				close(ch)
			}
			f.mu.Unlock()
		})
	}
	return ch, unsubscribe
}

func (f *rxFanout) dispatch(msg CanFrame) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	for ch := range f.subs {
		select {
		case ch <- msg:
		default:
			f.telemetry.reportSubscriberDrop()
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
