package preset

import (
	"context"
	"sync"
	"testing"

	"github.com/LoveWonYoung/canbuskit/driver"
)

type presetMockDriver struct {
	mu      sync.Mutex
	rxChan  chan driver.UnifiedCANMessage
	rxCalls int
	ctx     context.Context
}

func newPresetMockDriver() *presetMockDriver {
	return &presetMockDriver{
		rxChan: make(chan driver.UnifiedCANMessage, 1),
		ctx:    context.Background(),
	}
}

func (m *presetMockDriver) Init() error              { return nil }
func (m *presetMockDriver) Start()                   {}
func (m *presetMockDriver) Stop()                    {}
func (m *presetMockDriver) Context() context.Context { return m.ctx }
func (m *presetMockDriver) Write(id int32, fd bool, data []byte) error {
	return nil
}

func (m *presetMockDriver) RxChan() <-chan driver.UnifiedCANMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rxCalls++
	return m.rxChan
}

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
