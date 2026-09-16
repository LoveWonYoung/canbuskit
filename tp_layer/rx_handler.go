package tp_layer

import (
	"context"
	"errors"
	"fmt"
)

// ProcessRx Modified to take txChan to allow sending FlowControl frames directly
func (t *Transport) ProcessRx(msg CanMessage, txChan chan<- CanMessage) {
	t.processRx(context.Background(), msg, txChan)
}

func (t *Transport) processRx(ctx context.Context, msg CanMessage, txChan chan<- CanMessage) {
	if !t.address.IsForMe(&msg) {
		return
	}
	frame, err := ParseFrame(&msg)
	if err != nil {
		t.fireError(fmt.Errorf("报文解析失败: %v", err))
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

func (t *Transport) handleRxSingleFrame(f *SingleFrame) {
	if t.rxState != StateIdle {
		t.fireError(errors.New("警告：在多帧接收过程中被一个新单帧打断"))
	}
	t.stopReceiving()
	select {
	case t.rxDataChan <- f.Data:
	default:
		t.fireError(errors.New("ISO-TP receive queue is full; dropping single frame"))
	}
}

func (t *Transport) handleRxFirstFrame(ctx context.Context, f *FirstFrame, txChan chan<- CanMessage) {
	if t.rxState != StateIdle {
		t.fireError(errors.New("警告：在多帧接收过程中被一个新首帧打断"))
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

	t.rxState = StateWaitCF
	t.rxSeqNum = 1
	if err := t.sendFlowControlContext(ctx, FlowStatusContinueToSend, txChan); err != nil {
		t.stopReceiving()
		return
	}
	t.resetRxTimer()
}

func (t *Transport) handleRxConsecutiveFrame(ctx context.Context, f *ConsecutiveFrame, txChan chan<- CanMessage) {
	if t.rxState != StateWaitCF {
		// Ignore unexpected CF
		return
	}

	if f.SequenceNumber != t.rxSeqNum {
		t.fireError(fmt.Errorf("错误：序列号不匹配。期望: %d,收到: %d", t.rxSeqNum, f.SequenceNumber))
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

func (t *Transport) resetRxTimer() {
	stopTimer(t.timerRxCF)
	t.timerRxCF.Reset(t.config.TimeoutN_Cr)
}

func (t *Transport) sendFlowControl(status FlowStatus, txChan chan<- CanMessage) {
	_ = t.sendFlowControlContext(context.Background(), status, txChan)
}

func (t *Transport) sendFlowControlContext(ctx context.Context, status FlowStatus, txChan chan<- CanMessage) error {
	if t.isManualFlowControl() {
		return nil
	}

	msg := t.makeFlowControlMsg(status)
	return sendMessage(ctx, txChan, msg)
}

func (t *Transport) isManualFlowControl() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.manualFlowControl
}

func (t *Transport) makeFlowControlMsg(status FlowStatus) CanMessage {
	blockSize, stMin := t.flowControlDefaults()
	payload := createFlowControlPayload(status, blockSize, stMin)
	return t.makeTxMsgWithAddr(t.address, payload)
}

func (t *Transport) flowControlDefaults() (blockSize, stMin int) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.config.BlockSize, t.config.StMin
}
