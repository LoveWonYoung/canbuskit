package preset

import (
	"bytes"
	"sync"
	"testing"
	"time"

	"github.com/LoveWonYoung/canbuskit/driver"
)

type presetMockDriver struct {
	mu      sync.Mutex
	rxChan  chan driver.CanFrame
	writes  chan []byte
	rxCalls int
}

func newPresetMockDriver() *presetMockDriver {
	return &presetMockDriver{
		rxChan: make(chan driver.CanFrame, 1),
		writes: make(chan []byte, 2),
	}
}

func (m *presetMockDriver) Init() error { return nil }
func (m *presetMockDriver) Start()      {}
func (m *presetMockDriver) Stop()       {}
func (m *presetMockDriver) Write(id int32, fd bool, data []byte) error {
	m.writes <- append([]byte(nil), data...)
	return nil
}

func (m *presetMockDriver) RxChan() <-chan driver.CanFrame {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rxCalls++
	return m.rxChan
}

func (m *presetMockDriver) IsFDMode() bool { return false }

func (m *presetMockDriver) rxCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rxCalls
}

func TestReadReusesRxSubscription(t *testing.T) {
	drv := newPresetMockDriver()
	p := &Preset{CanDevice: drv}

	first := p.Read()
	second := p.Read()

	if first == nil {
		t.Fatal("Read returned nil channel")
	}
	if first != second {
		t.Fatal("Read should return the same channel on repeated calls")
	}
	if got := drv.rxCallCount(); got != 1 {
		t.Fatalf("expected one RxChan subscription, got %d", got)
	}
}

func TestPresetForwardsFlowControlSettings(t *testing.T) {
	drv := newPresetMockDriver()
	p, err := newPreset(drv, 0x7C6, 0x7C7, 0x7DF)
	if err != nil {
		t.Fatalf("newPreset() failed: %v", err)
	}
	defer p.Close()

	if err := p.SetDefaultBlockSize(30); err != nil {
		t.Fatalf("SetDefaultBlockSize() failed: %v", err)
	}
	if err := p.SetDefaultStMin(5); err != nil {
		t.Fatalf("SetDefaultStMin() failed: %v", err)
	}
	p.SetManualFlowControl(true)

	firstFrame := driver.CanFrame{
		Direction: driver.RX,
		ID:        p.RespId,
		DLC:       8,
	}
	copy(firstFrame.Data[:], []byte{0x10, 0x0A, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06})
	drv.rxChan <- firstFrame

	select {
	case data := <-drv.writes:
		t.Fatalf("manual mode emitted an automatic flow-control frame: % X", data)
	case <-time.After(50 * time.Millisecond):
	}

	p.SetManualFlowControl(false)
	drv.rxChan <- firstFrame
	select {
	case data := <-drv.writes:
		if !bytes.Equal(data[:3], []byte{0x30, 0x1E, 0x05}) {
			t.Fatalf("unexpected automatic flow-control frame: % X", data)
		}
	case <-time.After(time.Second):
		t.Fatal("automatic flow-control frame was not restored")
	}
}
