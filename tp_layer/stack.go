package tp_layer

import (
	"context"
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

	// Native timers
	timerRxCF    *time.Timer
	timerRxFC    *time.Timer
	timerTxSTmin *time.Timer

	// Configuration
	config            Config
	manualFlowControl bool

	// Error Channel
	ErrorChan chan error
}

func NewTransport(address *Address, cfg Config) *Transport {
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
	t.txAddress = addr
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

// Send sends data. It might block if the send buffer is full.
func (t *Transport) Send(data []byte) {
	t.txDataChan <- data
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

// Run starts the protocol stack event loop.
func (t *Transport) Run(ctx context.Context, rxChan <-chan CanMessage, txChan chan<- CanMessage) {
	defer t.cleanup()

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
			t.ProcessRx(msg, txChan)
		case data, ok := <-txDataEnable:
			if !ok {
				return
			}
			t.initiateTx(data, txChan)
		case <-t.timerRxCF.C:
			fmt.Println("接收连续帧超时，重置接收状态。")
			t.stopReceiving()
		case <-t.timerRxFC.C:
			fmt.Println("等待流控帧超时，停止发送。")
			t.stopSending()
		case <-t.timerTxSTmin.C:
			if t.txState != StateTransmit {
				continue
			}
			t.handleTxTransmit(txChan)
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
	if !t.timerRxCF.Stop() {
		select {
		case <-t.timerRxCF.C:
		default:
		}
	}
}

func (t *Transport) stopSending() {
	t.txState = StateIdle
	t.txBuffer = nil
	t.txFrameLen = 0
	t.txSeqNum = 0
	t.txBlockCounter = 0
	if !t.timerRxFC.Stop() {
		select {
		case <-t.timerRxFC.C:
		default:
		}
	}
	if !t.timerTxSTmin.Stop() {
		select {
		case <-t.timerTxSTmin.C:
		default:
		}
	}
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

	fullPayload := append([]byte(nil), data...)

	// Padding
	if t.config.PaddingByte != nil {
		targetLen := 8
		if isFD {
			targetLen = nextFDTargetLength(len(fullPayload))
		}

		if len(fullPayload) < targetLen {
			padding := make([]byte, targetLen-len(fullPayload))
			for i := range padding {
				padding[i] = *t.config.PaddingByte
			}
			fullPayload = append(fullPayload, padding...)
		}
	}

	return CanMessage{
		ArbitrationID: addr.TxID,
		Data:          fullPayload,
		IsFD:          isFD,
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
	select {
	case t.ErrorChan <- err:
	default:
		fmt.Println("ISOTP Error (Chan Full):", err)
	}
}
