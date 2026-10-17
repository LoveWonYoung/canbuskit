package isotp

import (
	"context"
	"errors"
	"fmt"
)

func (t *Session) processRx(ctx context.Context, msg linkFrame, txChan chan<- linkFrame) {
	if !t.address.Accepts(msg.ArbitrationID) {
		return
	}
	frame, err := ParseFrame(msg.Data)
	if err != nil {
		t.fireError(fmt.Errorf("parse ISO-TP frame: %w", err))
		return
	}

	switch f := frame.(type) {
	case *FlowControlFrame:
		t.handleTxFlowControl(f)

	case *SingleFrame:
		t.handleRxSingleFrame(f)

	case *FirstFrame:
		t.handleRxFirstFrame(ctx, f, txChan)

	case *ConsecutiveFrame:
		t.handleRxConsecutiveFrame(ctx, f, txChan)
	}
}

func (t *Session) handleRxSingleFrame(f *SingleFrame) {
	if t.rxState != stateIdle {
		t.fireError(errors.New("single frame interrupted an active multi-frame reception"))
	}
	t.stopReceiving()
	select {
	case t.rxDataChan <- f.Data:
	default:
		t.fireError(errors.New("ISO-TP receive queue is full; dropping single frame"))
	}
}

func (t *Session) handleRxFirstFrame(ctx context.Context, f *FirstFrame, txChan chan<- linkFrame) {
	if t.rxState != stateIdle {
		t.fireError(errors.New("first frame interrupted an active multi-frame reception"))
	}
	t.stopReceiving()
	if f.TotalSize <= len(f.Data) {
		t.fireError(fmt.Errorf("invalid ISO-TP first-frame length %d for %d data bytes", f.TotalSize, len(f.Data)))
		return
	}
	if f.TotalSize > t.config.MaxPayloadSize {
		t.fireError(fmt.Errorf("ISO-TP payload length %d exceeds configured maximum %d", f.TotalSize, t.config.MaxPayloadSize))
		return
	}

	t.rxFrameLen = f.TotalSize
	t.rxBuffer = make([]byte, 0, f.TotalSize) // Optimize allocation
	t.rxBuffer = append(t.rxBuffer, f.Data...)

	t.rxState = stateWaitCF
	t.rxSeqNum = 1
	if err := t.sendFlowControlContext(ctx, FlowStatusContinueToSend, txChan); err != nil {
		t.stopReceiving()
		return
	}
	t.resetRxTimer()
}

func (t *Session) handleRxConsecutiveFrame(ctx context.Context, f *ConsecutiveFrame, txChan chan<- linkFrame) {
	if t.rxState != stateWaitCF {
		// Ignore unexpected CF
		return
	}

	if f.SequenceNumber != t.rxSeqNum {
		t.fireError(fmt.Errorf("consecutive-frame sequence mismatch: expected %d, got %d", t.rxSeqNum, f.SequenceNumber))
		t.stopReceiving()
		return
	}

	t.resetRxTimer()
	t.rxSeqNum = (t.rxSeqNum + 1) % 16

	bytesToReceive := t.rxFrameLen - len(t.rxBuffer)
	if len(f.Data) > bytesToReceive {
		t.rxBuffer = append(t.rxBuffer, f.Data[:bytesToReceive]...)
	} else {
		t.rxBuffer = append(t.rxBuffer, f.Data...)
	}

	if len(t.rxBuffer) >= t.rxFrameLen {
		completedData := make([]byte, len(t.rxBuffer))
		copy(completedData, t.rxBuffer)
		select {
		case t.rxDataChan <- completedData:
		default:
			t.fireError(errors.New("ISO-TP receive queue is full; dropping reassembled payload"))
		}
		t.stopReceiving()
	} else {
		t.rxBlockCounter++
		blockSize, _ := t.flowControlDefaults()
		if blockSize > 0 && t.rxBlockCounter >= blockSize {
			t.rxBlockCounter = 0
			if err := t.sendFlowControlContext(ctx, FlowStatusContinueToSend, txChan); err != nil {
				t.stopReceiving()
				return
			}
			t.resetRxTimer()
		}
	}
}

func (t *Session) resetRxTimer() {
	stopTimer(t.timerRxCF)
	t.timerRxCF.Reset(t.config.ConsecutiveFrameTimeout)
}

func (t *Session) sendFlowControl(status FlowStatus, txChan chan<- linkFrame) {
	_ = t.sendFlowControlContext(context.Background(), status, txChan)
}

func (t *Session) sendFlowControlContext(ctx context.Context, status FlowStatus, txChan chan<- linkFrame) error {
	if t.isManualFlowControl() {
		return nil
	}

	msg := t.makeFlowControlMsg(status)
	return sendMessage(ctx, txChan, msg)
}

func (t *Session) isManualFlowControl() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.config.ManualFlowControl
}

func (t *Session) makeFlowControlMsg(status FlowStatus) linkFrame {
	blockSize, stMin := t.flowControlDefaults()
	payload := createFlowControlPayload(status, blockSize, stMin)
	return t.makeTxMsgWithAddr(t.address, payload)
}

func (t *Session) flowControlDefaults() (blockSize, stMin int) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.config.BlockSize, t.config.STMin
}
