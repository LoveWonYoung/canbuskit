package tp_layer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Transport 是ISOTP协议栈的核心结构
type Transport struct {
	address       *Address
	txAddress     *Address
	IsFD          bool
	MaxDataLength int
	mu            sync.RWMutex
	rxState       State
	txState       State
	rxBuffer      []byte
	txBuffer      []byte

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
	config            Config
	configErr         error
	manualFlowControl bool

	// Error Channel
	ErrorChan chan error
}

func NewTransport(address *Address, cfg Config) *Transport {
	normalized, configErr := normalizeConfig(cfg)
	cfg = normalized
	if err := address.Validate(); err != nil {
		configErr = errors.Join(configErr, err)
	} else {
		address = &Address{TxID: address.TxID, RxID: address.RxID}
	}
	t := &Transport{
		address:       address,
		rxDataChan:    make(chan []byte, 10), // Buffer size can be tuned
		txDataChan:    make(chan []byte, 10),
		IsFD:          false,
		MaxDataLength: 8,
		// Initialize timers with config values, but stopped
		timerRxCF:    time.NewTimer(time.Hour),
		timerRxFC:    time.NewTimer(time.Hour),
		timerTxSTmin: time.NewTimer(time.Hour),
		config:       cfg,
		configErr:    configErr,
		ErrorChan:    make(chan error, 10),
	}
	t.timerRxCF.Stop()
	t.timerRxFC.Stop()
	t.timerTxSTmin.Stop()

	t.stopReceiving()
	t.stopSending()
	return t
}

// SetTxAddress allows switching the transmit address without affecting RX filtering.
// When nil, the transport uses the base address provided at construction time.
func (t *Transport) SetTxAddress(addr *Address) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if addr == nil {
		t.txAddress = nil
		return
	}
	t.txAddress = &Address{TxID: addr.TxID, RxID: addr.RxID}
}

func (t *Transport) SetFDMode(isFD bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.IsFD = isFD
	if isFD {
		t.MaxDataLength = 64
	} else {
		t.MaxDataLength = 8
	}
}

// SetDefaultStMin sets the STmin value, in milliseconds, advertised by
// automatically sent flow-control frames.
func (t *Transport) SetDefaultStMin(stMin int) error {
	if stMin < 0 || stMin > 127 {
		return fmt.Errorf("STmin must be between 0 and 127 milliseconds: %d", stMin)
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.config.StMin = stMin
	return nil
}

// SetDefaultBlockSize sets the block size advertised by automatically sent
// flow-control frames. A value of 0 allows all remaining consecutive frames
// without another flow-control frame.
func (t *Transport) SetDefaultBlockSize(blockSize int) error {
	if blockSize < 0 || blockSize > 255 {
		return fmt.Errorf("block size must be between 0 and 255: %d", blockSize)
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.config.BlockSize = blockSize
	return nil
}

// SetManualFlowControl controls whether flow-control frames are sent by the
// transport. When enabled, the transport keeps its receive state and timers but
// does not automatically send flow-control frames; the caller is responsible
// for sending them through the CAN driver. Automatic flow control is enabled by
// default.
func (t *Transport) SetManualFlowControl(enabled bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.manualFlowControl = enabled
}

// Send sends a copy of data. It might block if the send buffer is full.
func (t *Transport) Send(data []byte) {
	_ = t.SendContext(context.Background(), data)

}

// SendContext queues a copy of data or returns when ctx is cancelled.
func (t *Transport) SendContext(ctx context.Context, data []byte) error {
	if ctx == nil {
		return errors.New("send context cannot be nil")
	}
	if t.configErr != nil {
		return t.configErr
	}
	if len(data) > t.config.MaxPayloadSize {
		return fmt.Errorf("ISO-TP payload length %d exceeds configured maximum %d", len(data), t.config.MaxPayloadSize)
	}
	payload := append([]byte(nil), data...)
	select {
	case t.txDataChan <- payload:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Recv receives data. It matches the old signature but now pulls from channel.
func (t *Transport) Recv() ([]byte, bool) {
	select {
	case data, ok := <-t.rxDataChan:
		if !ok {
			return nil, false
		}
		return data, true
	default:
		return nil, false
	}
}

// RecvChan returns a receive-only channel for blocking reads by callers.
func (t *Transport) RecvChan() <-chan []byte {
	return t.rxDataChan
}

// Errors returns asynchronous protocol and queue errors.
func (t *Transport) Errors() <-chan error {
	return t.ErrorChan
}

// Run starts the protocol stack event loop.
func (t *Transport) Run(ctx context.Context, rxChan <-chan CanMessage, txChan chan<- CanMessage) {
	defer t.cleanup()
	if t.configErr != nil {
		t.fireError(t.configErr)
		return
	}

	for {
		var txDataEnable <-chan []byte
		if t.txState == StateIdle {
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
			if t.txState != StateTransmit {
				continue
			}
			t.handleTxTransmit(ctx, txChan)
		}
	}
}

func (t *Transport) cleanup() {
	t.timerRxCF.Stop()
	t.timerRxFC.Stop()
	t.timerTxSTmin.Stop()
}

// Internal helpers
func (t *Transport) stopReceiving() {
	t.rxState = StateIdle
	t.rxBuffer = nil
	t.rxFrameLen = 0
	t.rxSeqNum = 0
	t.rxBlockCounter = 0
	stopTimer(t.timerRxCF)
}

func (t *Transport) stopSending() {
	t.txState = StateIdle
	t.txBuffer = nil
	t.txFrameLen = 0
	t.txSeqNum = 0
	t.txBlockCounter = 0
	t.txWaitFrames = 0
	stopTimer(t.timerRxFC)
	stopTimer(t.timerTxSTmin)
}

func (t *Transport) makeTxMsg(data []byte) CanMessage {
	t.mu.RLock()
	addr := t.txAddress
	if addr == nil {
		addr = t.address
	}
	t.mu.RUnlock()
	return t.makeTxMsgWithAddr(addr, data)
}

func (t *Transport) makeTxMsgWithAddr(addr *Address, data []byte) CanMessage {
	t.mu.RLock()
	isFD := t.IsFD
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

	return CanMessage{
		ArbitrationID: addr.TxID,
		Data:          fullPayload,
		IsFD:          isFD,
	}
}

func sendMessage(ctx context.Context, txChan chan<- CanMessage, msg CanMessage) error {
	select {
	case txChan <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (t *Transport) maxDataLength() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.MaxDataLength
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

// fireError sends an error to the ErrorChan. Non-blocking.
func (t *Transport) fireError(err error) {
	if err == nil {
		return
	}
	select {
	case t.ErrorChan <- err:
	default:
	}
}
