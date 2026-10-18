package backend

import (
	"context"
	"fmt"
	"sync"

	"github.com/LoveWonYoung/canbuskit/v2/can"
)

// logCANMessageRelative records time since the first hardware frame and time
// since the preceding hardware frame.
func logCANMessageRelative(cfg can.Config, direction string, id uint32, dlc byte, data []byte, canType can.Mode, elapsedUS, deltaUS uint64) {
	if cfg.Logger == nil {
		return
	}
	typeStr := "CANFD"
	if canType == can.Classic {
		typeStr = "CAN  "
	}
	elapsedSeconds := elapsedUS / 1_000_000
	elapsedMilliseconds := elapsedUS / 1_000 % 1_000
	elapsedMicroseconds := elapsedUS % 1_000
	deltaSeconds := deltaUS / 1_000_000
	deltaMilliseconds := deltaUS / 1_000 % 1_000
	deltaMicroseconds := deltaUS % 1_000
	format := "%s %s: Elapsed=%ds %03dms %03dus, Delta=%ds %03dms %03dus, ID=0x%03X, DLC=%02d, Data=% 02X"
	cfg.Logger.Info(fmt.Sprintf(format, direction, typeStr,
		elapsedSeconds, elapsedMilliseconds, elapsedMicroseconds,
		deltaSeconds, deltaMilliseconds, deltaMicroseconds,
		id, dlc, data))
}

func logf(cfg can.Config, format string, args ...any) {
	if cfg.Logger != nil {
		cfg.Logger.Info(fmt.Sprintf(format, args...))
	}
}

// dlcLen returns the payload length represented by a DLC. Invalid DLC values
// yield 0 so a slice bound stays in range.
func dlcLen(dlc byte) int {
	n, _ := can.DLCToLength(dlc)
	return n
}

// device is the low-level contract implemented by vendor backends. bus adds
// context-aware lifecycle, frame validation, and subscription semantics.
type device interface {
	initialize() error
	start() error
	stop()
	write(can.Frame) error
	subscribe(buffer int) (<-chan can.Frame, func())
	isFD() bool
	config() can.Config
	setBRS(bool)
	brs() bool
	errors() <-chan error
	stats() can.Stats
	busLoad() can.BusLoad
}

// lifecycle serializes initialization and cleanup and makes the read loop
// start/stop sequence idempotent.
type lifecycle struct {
	opMu        sync.Mutex
	mu          sync.Mutex
	readWG      sync.WaitGroup
	initialized bool
	running     bool
}

func (l *lifecycle) isInitialized() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.initialized
}

func (l *lifecycle) markInitialized() {
	l.mu.Lock()
	l.initialized = true
	l.mu.Unlock()
}

func (l *lifecycle) start(readLoop func()) bool {
	l.mu.Lock()
	if !l.initialized || l.running {
		l.mu.Unlock()
		return false
	}
	l.running = true
	l.readWG.Add(1)
	l.mu.Unlock()

	go func() {
		defer func() {
			l.mu.Lock()
			l.running = false
			l.mu.Unlock()
			l.readWG.Done()
		}()
		readLoop()
	}()
	return true
}

// cancelAndWait marks the instance stopped, calls cancel, and waits until the
// hardware read loop is no longer executing vendor code.
func (l *lifecycle) cancelAndWait(cancel context.CancelFunc) bool {
	l.mu.Lock()
	wasInitialized := l.initialized
	l.initialized = false
	l.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	l.readWG.Wait()
	return wasInitialized
}
