package tp_layer

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Transport 是ISOTP协议栈的核心结构
type Transport struct {
	address       *Address
	txAddress     *Address
	IsFD          bool
	MaxDataLength int
	rxState       State
	txState       State
	rxBuffer      []byte
	txBuffer      []byte

	// Channels replaced queues
	rxDataChan chan []byte
	txDataChan chan []byte

	rxFrameLen           int
	txFrameLen           int
	rxSeqNum             int
	txSeqNum             int
	rxBlockCounter       int
	txBlockCounter       int
	remoteBlocksize      int
	remoteStmin          time.Duration
	lastFlowControlFrame *FlowControlFrame
	pendingFlowControlTx bool

	// Native timers
	timerRxCF    *time.Timer
	timerRxFC    *time.Timer
	timerTxSTmin *time.Timer

	// Configuration
	config Config

	// Runtime State
	wftCounter int

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
	t.txAddress = addr
}

func (t *Transport) SetFDMode(isFD bool) {
	t.IsFD = isFD
	if isFD {
		t.MaxDataLength = 64
	} else {
		t.MaxDataLength = 8
	}
}

// Send sends data. It might block if the send buffer is full.
func (t *Transport) Send(data []byte) {
	t.txDataChan <- data
}

// Recv receives data. It matches the old signature but now pulls from channel.
func (t *Transport) Recv() ([]byte, bool) {
	select {
	case data := <-t.rxDataChan:
		return data, true
	default:
		return nil, false
	}
}

// Run starts the protocol stack event loop.
func (t *Transport) Run(ctx context.Context, rxChan <-chan CanMessage, txChan chan<- CanMessage) {
	defer t.cleanup()

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-rxChan:
			t.ProcessRx(msg, txChan)
		case data := <-t.txDataChan:
			if t.txState == StateIdle {
				t.startTransmission(data, txChan)
			} else {
				t.fireError(errors.New("Error: Concurrent underlying send (logic error in select handling)"))
			}
		case <-t.timerRxCF.C:
			fmt.Println("接收连续帧超时，重置接收状态。")
			t.stopReceiving()
		case <-t.timerRxFC.C:
			fmt.Println("等待流控帧超时，停止发送。")
			t.stopSending()
		case <-t.timerTxSTmin.C:
			t.handleTxTransmit(txChan)
		}
	}
}

// RunOptimized is the loop with proper state handling
func (t *Transport) RunEventLoop(ctx context.Context, rxChan <-chan CanMessage, txChan chan<- CanMessage) {
	defer t.cleanup()

	for {
		var txDataEnable <-chan []byte
		if t.txState == StateIdle {
			txDataEnable = t.txDataChan
		}

		select {
		case <-ctx.Done():
			return

		case msg := <-rxChan:
			t.ProcessRx(msg, txChan)

		case data := <-txDataEnable:
			t.startTransmission(data, txChan)

		case <-t.timerRxCF.C:
			fmt.Println("接收连续帧超时，重置接收状态。")
			t.stopReceiving()

		case <-t.timerRxFC.C:
			fmt.Println("等待流控帧超时，停止发送。")
			t.stopSending()

		case <-t.timerTxSTmin.C:
			if t.txState == StateTransmit {
				t.handleTxTransmit(txChan)
			}
		}
	}
}

func (t *Transport) cleanup() {
	t.timerRxCF.Stop()
	t.timerRxFC.Stop()
	t.timerTxSTmin.Stop()
}

func (t *Transport) startTransmission(data []byte, txChan chan<- CanMessage) {
	t.initiateTx(data, txChan)
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

func (t *Transport) makeTxMsg(data []byte, addrType AddressType) CanMessage {
	addr := t.txAddress
	if addr == nil {
		addr = t.address
	}
	return t.makeTxMsgWithAddr(addr, data, addrType)
}

func (t *Transport) makeTxMsgWithAddr(addr *Address, data []byte, addrType AddressType) CanMessage {
	arbitrationID := addr.GetTxArbitrationID(addrType)
	fullPayload := append(addr.TxPayloadPrefix, data...)

	// Padding
	if t.config.PaddingByte != nil {
		targetLen := 8
		if t.IsFD {
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
		ArbitrationID: arbitrationID,
		Data:          fullPayload,
		IsExtendedID:  addr.Is29Bit(),
		IsFD:          t.IsFD,
	}
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
		// Channel full, drop error or maybe log to stdlib log?
		// For now we just drop to prevent blocking the stack.
		fmt.Println("ISOTP Error (Chan Full):", err)
	}
}
