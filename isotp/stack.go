package isotp

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/LoveWonYoung/canbuskit/v2/can"
)

// ErrSessionClosed is returned when work is submitted after Close.
var ErrSessionClosed = errors.New("ISO-TP session is closed")

// Session 是ISOTP协议栈的核心结构
type Session struct {
	bus             can.Driver
	address         *Address
	txAddress       *Address
	fd              bool
	frameDataLength int
	mu              sync.RWMutex
	rxState         state
	txState         state
	rxBuffer        []byte
	txBuffer        []byte

	// Channels replaced queues
	rxDataChan chan []byte
	txDataChan chan []byte

	rxFrameLen      int
	txFrameLen      int
	rxSeqNum        int
	txSeqNum        int
	rxBlockCounter  int
	txBlockCounter  int
	remoteBlockSize int
	remoteSTmin     time.Duration
	txWaitFrames    int

	// Native timers
	timerRxCF    *time.Timer
	timerRxFC    *time.Timer
	timerTxSTmin *time.Timer

	// Configuration
	config Config

	errorChan   chan error
	ctx         context.Context
	cancel      context.CancelFunc
	unsubscribe func()
	wg          sync.WaitGroup
	closeOnce   sync.Once
}

func newSession(address *Address, cfg Config, fd bool) (*Session, error) {
	normalized, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	cfg = normalized
	if err := address.Validate(); err != nil {
		return nil, err
	}
	address = &Address{TxID: address.TxID, RxID: address.RxID}
	frameDataLength := 8
	if fd {
		frameDataLength = 64
	}
	t := &Session{
		address:         address,
		rxDataChan:      make(chan []byte, 10), // Buffer size can be tuned
		txDataChan:      make(chan []byte, 10),
		fd:              fd,
		frameDataLength: frameDataLength,
		// Initialize timers with config values, but stopped
		timerRxCF:    time.NewTimer(time.Hour),
		timerRxFC:    time.NewTimer(time.Hour),
		timerTxSTmin: time.NewTimer(time.Hour),
		config:       cfg,
		errorChan:    make(chan error, 10),
	}
	t.timerRxCF.Stop()
	t.timerRxFC.Stop()
	t.timerTxSTmin.Stop()

	t.stopReceiving()
	t.stopSending()
	return t, nil
}

// New creates and starts an ISO-TP session over an already-open CAN driver.
// Closing the session does not close the driver.
func New(bus can.Driver, address *Address, cfg Config) (*Session, error) {
	if bus == nil {
		return nil, errors.New("CAN driver cannot be nil")
	}
	t, err := newSession(address, cfg, bus.Config().Mode == can.FD)
	if err != nil {
		return nil, err
	}
	t.bus = bus
	ctx, cancel := context.WithCancel(context.Background())
	t.ctx = ctx
	t.cancel = cancel
	rx, unsubscribe, err := bus.Subscribe(100)
	if err != nil {
		t.cleanup()
		return nil, fmt.Errorf("subscribe to CAN driver: %w", err)
	}
	t.unsubscribe = unsubscribe
	rxMessages := make(chan linkFrame, 100)
	tx := make(chan linkFrame, 1024)

	t.wg.Go(func() {
		for {
			select {
			case <-ctx.Done():
				return
			case frame, ok := <-rx:
				if !ok {
					return
				}
				message, accepted := fromCANFrame(frame)
				if !accepted {
					continue
				}
				select {
				case <-ctx.Done():
					return
				case rxMessages <- message:
				}
			}
		}
	})
	t.wg.Go(func() {
		for {
			select {
			case <-ctx.Done():
				return
			case message := <-tx:
				frame, err := can.NewFrame(message.ArbitrationID, message.Data, can.FrameFlags{
					FD:  message.FD,
					BRS: message.FD && bus.Config().BRS,
				})
				if err == nil {
					err = bus.Send(ctx, frame)
				}
				if err != nil && !errors.Is(err, context.Canceled) {
					t.fireError(fmt.Errorf("send ISO-TP CAN frame: %w", err))
				}
			}
		}
	})
	t.wg.Go(func() { t.run(ctx, rxMessages, tx) })
	return t, nil
}

func fromCANFrame(frame can.Frame) (linkFrame, bool) {
	if frame.Direction == can.Transmit || frame.ID > 0x7FF {
		return linkFrame{}, false
	}
	length, valid := frame.ValidDataLength()
	if !valid {
		return linkFrame{}, false
	}
	data := make([]byte, length)
	copy(data, frame.Data[:length])
	return linkFrame{ArbitrationID: frame.ID, Data: data, FD: frame.FD}, true
}

// Close stops this protocol session without closing its CAN driver.
func (t *Session) Close() error {
	if t == nil {
		return nil
	}
	t.closeOnce.Do(func() {
		if t.cancel != nil {
			t.cancel()
		}
		if t.unsubscribe != nil {
			t.unsubscribe()
		}
		t.wg.Wait()
		close(t.rxDataChan)
		close(t.errorChan)
	})
	return nil
}

// SetTxAddress allows switching the transmit address without affecting RX filtering.
// When nil, the transport uses the base address provided at construction time.
func (t *Session) SetTxAddress(addr *Address) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if addr == nil {
		t.txAddress = nil
		return nil
	}
	if err := addr.Validate(); err != nil {
		return err
	}
	t.txAddress = &Address{TxID: addr.TxID, RxID: addr.RxID}
	return nil
}

// FDMode reports the immutable framing mode.
func (t *Session) FDMode() bool {
	return t.fd
}

// MaxFrameDataLength returns 8 for classic CAN and 64 for CAN FD.
func (t *Session) MaxFrameDataLength() int {
	return t.frameDataLength
}

// SendContext queues a copy of data or returns when ctx is cancelled.
func (t *Session) SendContext(ctx context.Context, data []byte) error {
	if ctx == nil {
		return errors.New("send context cannot be nil")
	}
	if len(data) > t.config.MaxPayloadSize {
		return fmt.Errorf("ISO-TP payload length %d exceeds configured maximum %d", len(data), t.config.MaxPayloadSize)
	}
	payload := append([]byte(nil), data...)
	var sessionDone <-chan struct{}
	if t.ctx != nil {
		sessionDone = t.ctx.Done()
	}
	select {
	case <-sessionDone:
		return ErrSessionClosed
	default:
	}
	select {
	case t.txDataChan <- payload:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-sessionDone:
		return ErrSessionClosed
	}
}

// RecvChan returns a receive-only channel for blocking reads by callers.
func (t *Session) RecvChan() <-chan []byte {
	return t.rxDataChan
}

// Errors returns asynchronous protocol and queue errors.
func (t *Session) Errors() <-chan error {
	return t.errorChan
}

func (t *Session) run(ctx context.Context, rxChan <-chan linkFrame, txChan chan<- linkFrame) {
	defer t.cleanup()
	for {
		var txDataEnable <-chan []byte
		if t.txState == stateIdle {
			txDataEnable = t.txDataChan
		}

		select {
		case <-ctx.Done():
			return
		case msg, ok := <-rxChan:
			if !ok {
				return
			}
			t.processRx(ctx, msg, txChan)
		case data, ok := <-txDataEnable:
			if !ok {
				return
			}
			t.initiateTx(ctx, data, txChan)
		case <-t.timerRxCF.C:
			t.fireError(errors.New("timed out waiting for ISO-TP consecutive frame"))
			t.stopReceiving()
		case <-t.timerRxFC.C:
			t.fireError(errors.New("timed out waiting for ISO-TP flow-control frame"))
			t.stopSending()
		case <-t.timerTxSTmin.C:
			if t.txState != stateTransmit {
				continue
			}
			t.handleTxTransmit(ctx, txChan)
		}
	}
}

func (t *Session) cleanup() {
	t.timerRxCF.Stop()
	t.timerRxFC.Stop()
	t.timerTxSTmin.Stop()
}

// Internal helpers
func (t *Session) stopReceiving() {
	t.rxState = stateIdle
	t.rxBuffer = nil
	t.rxFrameLen = 0
	t.rxSeqNum = 0
	t.rxBlockCounter = 0
	stopTimer(t.timerRxCF)
}

func (t *Session) stopSending() {
	t.txState = stateIdle
	t.txBuffer = nil
	t.txFrameLen = 0
	t.txSeqNum = 0
	t.txBlockCounter = 0
	t.txWaitFrames = 0
	stopTimer(t.timerRxFC)
	stopTimer(t.timerTxSTmin)
}

func (t *Session) makeTxMsg(data []byte) linkFrame {
	t.mu.RLock()
	addr := t.txAddress
	if addr == nil {
		addr = t.address
	}
	t.mu.RUnlock()
	return t.makeTxMsgWithAddr(addr, data)
}

func (t *Session) makeTxMsgWithAddr(addr *Address, data []byte) linkFrame {
	t.mu.RLock()
	isFD := t.fd
	t.mu.RUnlock()

	// Frame constructors return newly owned slices, so the transport can pass
	// them through without another per-frame copy.
	fullPayload := data

	// Padding
	if t.config.PaddingByte != nil {
		targetLen := 8
		if isFD {
			targetLen = nextFDTargetLength(len(fullPayload))
		}

		if len(fullPayload) < targetLen {
			originalLen := len(fullPayload)
			if cap(fullPayload) >= targetLen {
				fullPayload = fullPayload[:targetLen]
			} else {
				padded := make([]byte, targetLen)
				copy(padded, fullPayload)
				fullPayload = padded
			}
			for i := originalLen; i < targetLen; i++ {
				fullPayload[i] = *t.config.PaddingByte
			}
		}
	}

	return linkFrame{
		ArbitrationID: addr.TxID,
		Data:          fullPayload,
		FD:            isFD,
	}
}

func sendMessage(ctx context.Context, txChan chan<- linkFrame, msg linkFrame) error {
	select {
	case txChan <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (t *Session) maxDataLength() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.frameDataLength
}

// nextFDTargetLength returns the smallest CAN FD data length that is >= length.
// Valid CAN FD payload sizes are 0-8, 12, 16, 20, 24, 32, 48, 64.
func nextFDTargetLength(length int) int {
	if length <= 8 {
		return 8
	}
	switch {
	case length <= 12:
		return 12
	case length <= 16:
		return 16
	case length <= 20:
		return 20
	case length <= 24:
		return 24
	case length <= 32:
		return 32
	case length <= 48:
		return 48
	default:
		return 64
	}
}

// fireError sends an error to the error channel without blocking the state machine.
func (t *Session) fireError(err error) {
	if err == nil {
		return
	}
	select {
	case t.errorChan <- err:
	default:
	}
}
