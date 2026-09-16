package preset

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/LoveWonYoung/canbuskit/driver"
	"github.com/LoveWonYoung/canbuskit/tp_layer"
)

type presetMockDriver struct {
	mu          sync.Mutex
	rxChan      chan driver.CanFrame
	writes      chan []byte
	rxCalls     int
	initCalls   int
	startCalls  int
	stopCalls   int
	lastWriteID int32
	lastWriteFD bool
}

func newPresetMockDriver() *presetMockDriver {
	return &presetMockDriver{
		rxChan: make(chan driver.CanFrame, 1),
		writes: make(chan []byte, 8),
	}
}

func (m *presetMockDriver) Init() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.initCalls++
	return nil
}

func (m *presetMockDriver) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startCalls++
}

func (m *presetMockDriver) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopCalls++
}
func (m *presetMockDriver) Write(id int32, fd bool, data []byte) error {
	m.mu.Lock()
	m.lastWriteID = id
	m.lastWriteFD = fd
	m.mu.Unlock()
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

func (m *presetMockDriver) lifecycleCallCounts() (init, start, stop int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.initCalls, m.startCalls, m.stopCalls
}

func (m *presetMockDriver) lastWrite() (id int32, fd bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastWriteID, m.lastWriteFD
}

func TestPresetDefaultsToRawCANMode(t *testing.T) {
	drv := newPresetMockDriver()
	p, err := newPreset(drv, 0x7C6, 0x7C7, 0x7DF)
	if err != nil {
		t.Fatalf("newPreset() failed: %v", err)
	}

	if p.Client != nil {
		t.Fatal("UDS client should be disabled by default")
	}
	if got := drv.rxCallCount(); got != 0 {
		t.Fatalf("raw mode should not register a UDS receive subscription, got %d calls", got)
	}
	if init, start, stop := drv.lifecycleCallCounts(); init != 1 || start != 1 || stop != 0 {
		t.Fatalf("unexpected lifecycle counts before close: init=%d start=%d stop=%d", init, start, stop)
	}
	if _, err := p.Request([]byte{0x22, 0xF1, 0x90}, time.Second); !errors.Is(err, ErrUDSClientDisabled) {
		t.Fatalf("Request() error = %v, want ErrUDSClientDisabled", err)
	}

	p.Close()
	p.Close()
	if _, _, stop := drv.lifecycleCallCounts(); stop != 1 {
		t.Fatalf("Close() should stop the raw CAN device once, got %d calls", stop)
	}
}

func TestPresetRegistersUDSClientOnlyWhenEnabled(t *testing.T) {
	drv := newPresetMockDriver()
	p, err := newPreset(drv, 0x7C6, 0x7C7, 0x7DF, WithUDSClient(true))
	if err != nil {
		t.Fatalf("newPreset() failed: %v", err)
	}
	defer p.Close()

	if p.Client == nil {
		t.Fatal("UDS client should be initialized when explicitly enabled")
	}
	if got := drv.rxCallCount(); got != 1 {
		t.Fatalf("UDS mode should register one receive subscription, got %d calls", got)
	}
}

func TestPresetWritesManualTPFramesWithoutUDSClient(t *testing.T) {
	drv := newPresetMockDriver()
	p, err := newPreset(drv, 0x7C6, 0x7C7, 0x7DF)
	if err != nil {
		t.Fatalf("newPreset() failed: %v", err)
	}
	defer p.Close()
	if p.Client != nil {
		t.Fatal("manual TP test unexpectedly initialized a UDS client")
	}

	tests := []struct {
		name  string
		write func() error
		want  []byte
	}{
		{
			name: "single frame",
			write: func() error {
				return p.WriteTPSingleFrame(0x7C6, false, []byte{0x22, 0xF1, 0x90})
			},
			want: []byte{0x03, 0x22, 0xF1, 0x90},
		},
		{
			name: "first frame",
			write: func() error {
				return p.WriteTPFirstFrame(0x7C6, false, []byte{1, 2, 3, 4, 5, 6}, 20)
			},
			want: []byte{0x10, 0x14, 1, 2, 3, 4, 5, 6},
		},
		{
			name: "flow control frame",
			write: func() error {
				return p.WriteTPFlowControlFrame(0x7C6, false, tp_layer.FlowStatusContinueToSend, 8, 5)
			},
			want: []byte{0x30, 0x08, 0x05},
		},
		{
			name: "consecutive frame",
			write: func() error {
				return p.WriteTPConsecutiveFrame(0x7C6, false, []byte{7, 8, 9}, 1)
			},
			want: []byte{0x21, 7, 8, 9},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.write(); err != nil {
				t.Fatal(err)
			}
			got := <-drv.writes
			if !bytes.Equal(got, tc.want) {
				t.Fatalf("written frame = % X, want % X", got, tc.want)
			}
			if id, fd := drv.lastWrite(); id != 0x7C6 || fd {
				t.Fatalf("write metadata = (0x%X, %t), want (0x7C6, false)", id, fd)
			}
		})
	}
	if got := drv.rxCallCount(); got != 0 {
		t.Fatalf("manual TP writes should not register a receive subscription, got %d calls", got)
	}
}

func TestPresetManualTPPaddingSwitch(t *testing.T) {
	drv := newPresetMockDriver()
	p, err := newPreset(
		drv, 0x7C6, 0x7C7, 0x7DF,
		WithTPPadding(true),
	)
	if err != nil {
		t.Fatalf("newPreset() failed: %v", err)
	}
	defer p.Close()

	if enabled, paddingByte := p.TPPadding(); !enabled || paddingByte != defaultPaddingByte {
		t.Fatalf("TPPadding() = (%t, 0x%02X), want (true, 0x%02X)", enabled, paddingByte, defaultPaddingByte)
	}
	if err := p.WriteTPSingleFrame(0x7C6, false, []byte{0x22, 0xF1, 0x90}); err != nil {
		t.Fatal(err)
	}
	if got, want := <-drv.writes, []byte{0x03, 0x22, 0xF1, 0x90, 0xAA, 0xAA, 0xAA, 0xAA}; !bytes.Equal(got, want) {
		t.Fatalf("padded Single Frame = % X, want % X", got, want)
	}

	p.SetTPPaddingByte(0x00)
	if err := p.WriteTPFlowControlFrame(0x7C6, false, tp_layer.FlowStatusContinueToSend, 8, 5); err != nil {
		t.Fatal(err)
	}
	if got, want := <-drv.writes, []byte{0x30, 0x08, 0x05, 0, 0, 0, 0, 0}; !bytes.Equal(got, want) {
		t.Fatalf("padded Flow Control Frame = % X, want % X", got, want)
	}

	p.SetTPPadding(false)
	if err := p.WriteTPConsecutiveFrame(0x7C6, false, []byte{7, 8, 9}, 1); err != nil {
		t.Fatal(err)
	}
	if got, want := <-drv.writes, []byte{0x21, 7, 8, 9}; !bytes.Equal(got, want) {
		t.Fatalf("unpadded Consecutive Frame = % X, want % X", got, want)
	}
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
	p, err := newPreset(drv, 0x7C6, 0x7C7, 0x7DF, WithUDSClient(true))
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
