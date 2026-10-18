package backend

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/LoveWonYoung/canbuskit/v2/can"
)

type fakeDevice struct {
	observability
	mu       sync.Mutex
	cfg      can.Config
	started  bool
	writes   int
	holdSub  chan struct{}
	entered  chan struct{}
	brsValue bool
}

func (d *fakeDevice) initialize() error {
	cfg, err := normalizeConfig(d.cfg)
	if err != nil {
		return err
	}
	d.mu.Lock()
	d.cfg = cfg
	d.mu.Unlock()
	d.prepare(cfg)
	return nil
}

func (d *fakeDevice) start() error {
	d.mu.Lock()
	d.started = true
	d.mu.Unlock()
	return nil
}

func (d *fakeDevice) stop() {
	d.mu.Lock()
	d.started = false
	d.mu.Unlock()
}

func (d *fakeDevice) write(frame can.Frame) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.started {
		return can.ErrNotOpen
	}
	d.writes++
	d.recordBusTx(frame)
	return nil
}

func (d *fakeDevice) subscribe(int) (<-chan can.Frame, func()) {
	if d.entered != nil {
		d.entered <- struct{}{}
	}
	if d.holdSub != nil {
		<-d.holdSub
	}
	ch := make(chan can.Frame, 1)
	return ch, func() { close(ch) }
}

func (d *fakeDevice) isFD() bool { return d.cfg.Mode == can.FD }

func (d *fakeDevice) config() can.Config {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.cfg
}

func (d *fakeDevice) setBRS(enabled bool) {
	d.mu.Lock()
	d.cfg.BRS = enabled
	d.brsValue = enabled
	d.mu.Unlock()
}

func (d *fakeDevice) brs() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.brsValue
}

func TestBusConfigOpenSendAndStableErrors(t *testing.T) {
	dev := &fakeDevice{cfg: can.DefaultConfig(can.FD, can.Channel2)}
	drv := newBus(dev)
	errs := drv.Errors()

	if _, _, err := drv.Subscribe(1); !errors.Is(err, can.ErrNotOpen) {
		t.Fatalf("Subscribe before Open = %v, want ErrNotOpen", err)
	}
	frame, err := can.NewFrame(0x123, []byte{0x01}, can.FrameFlags{FD: true, BRS: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := drv.Send(context.Background(), frame); !errors.Is(err, can.ErrNotOpen) {
		t.Fatalf("Send before Open = %v, want ErrNotOpen", err)
	}
	if err := drv.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := drv.Open(context.Background()); !errors.Is(err, can.ErrAlreadyOpen) {
		t.Fatalf("second Open = %v, want ErrAlreadyOpen", err)
	}
	if got := drv.Config(); got.Channel != can.Channel2 || got.NominalBitrate != 500_000 {
		t.Fatalf("Config() = %+v", got)
	}
	if drv.Errors() != errs {
		t.Fatal("Errors channel changed after Open")
	}
	if err := drv.Send(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
	if !dev.brs() {
		t.Fatal("Send did not apply frame BRS")
	}
	if got := drv.Stats().SourceDropped; got != 0 {
		t.Fatalf("SourceDropped = %d", got)
	}
	frames, unsubscribe, err := drv.Subscribe(1)
	if err != nil || frames == nil {
		t.Fatalf("Subscribe() = %v, %v", frames, err)
	}
	unsubscribe()
	if err := drv.Close(); err != nil {
		t.Fatal(err)
	}
	if drv.Errors() != errs {
		t.Fatal("Errors channel changed after Close")
	}
	select {
	case <-errs:
		t.Fatal("Errors channel was closed")
	default:
	}
	if err := drv.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	if drv.Errors() != errs {
		t.Fatal("Errors channel changed after reopen")
	}
	if err := drv.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSubscribeWaitsOutClose(t *testing.T) {
	dev := &fakeDevice{
		cfg:     can.DefaultConfig(can.Classic, can.Channel1),
		holdSub: make(chan struct{}),
		entered: make(chan struct{}, 1),
	}
	drv := newBus(dev)
	if err := drv.Open(context.Background()); err != nil {
		t.Fatal(err)
	}

	type result struct {
		frames <-chan can.Frame
		err    error
	}
	done := make(chan result, 1)
	go func() {
		frames, unsubscribe, err := drv.Subscribe(1)
		if unsubscribe != nil {
			unsubscribe()
		}
		done <- result{frames: frames, err: err}
	}()

	select {
	case <-dev.entered:
	case <-time.After(time.Second):
		t.Fatal("Subscribe did not enter the device")
	}

	closed := make(chan error, 1)
	go func() { closed <- drv.Close() }()
	select {
	case err := <-closed:
		t.Fatalf("Close returned while Subscribe still held the bus: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(dev.holdSub)

	select {
	case got := <-done:
		if got.err != nil || got.frames == nil {
			t.Fatalf("Subscribe() = %v, %v; want a channel", got.frames, got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("Subscribe did not return")
	}
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not return")
	}
}

func TestBusSendCloseRace(t *testing.T) {
	dev := &fakeDevice{cfg: can.DefaultConfig(can.FD, can.Channel1)}
	drv := newBus(dev)
	if err := drv.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	frame, err := can.NewFrame(0x42, []byte{0x11}, can.FrameFlags{FD: true})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = drv.Send(context.Background(), frame)
		}()
		go func() {
			defer wg.Done()
			frames, unsubscribe, err := drv.Subscribe(1)
			if err == nil {
				if frames == nil {
					t.Error("Subscribe returned a nil channel")
				}
				unsubscribe()
				return
			}
			if !errors.Is(err, can.ErrNotOpen) {
				t.Errorf("Subscribe error = %v", err)
			}
		}()
	}
	wg.Wait()
	if err := drv.Close(); err != nil {
		t.Fatal(err)
	}
}
