package tp_layer

import (
	"errors"
	"fmt"
)

// ProcessRx 处理接收到的单个CAN报文
// Modified to take txChan to allow sending FlowControl frames directly
func (t *Transport) ProcessRx(msg CanMessage, txChan chan<- CanMessage) {
	if !t.address.IsForMe(&msg) {
		return
	}

	// Timeout checks are now handled in Run(), so we don't check here.
	// We just handle the frame.

	frame, err := ParseFrame(&msg, t.address.RxPrefixSize)
	if err != nil {
		t.fireError(fmt.Errorf("报文解析失败: %v", err))
		return
	}

	switch f := frame.(type) {
	case *FlowControlFrame:
		t.lastFlowControlFrame = f
		if t.rxState == StateWaitCF {
			if f.FlowStatus == FlowStatusWait || f.FlowStatus == FlowStatusContinueToSend {
				t.resetRxTimer()
			}
		}
		// Flow Control affects Tx state, which is handled in Run -> tx_handler logic
		// But here we just store it. The Run loop will check pending flags or we should trigger something?
		// Actually, Tx Flow Control handling is for when WE are sending.
		// So receiving a Flow Control frame means we are the sender.
		// We should notify the Tx logic.
		// In the new design, 'lastFlowControlFrame' is checked by the Tx logic.
		// We may need to signal the Tx logic if it's waiting.
		// But the Tx logic runs in the same goroutine (Run loop).
		// So simply setting t.lastFlowControlFrame is enough, IF the Tx logic checks it.
		// However, if Tx logic is blocked on a timer or channel, it might not notice immediately?
		// The Tx logic in Run() is event driven.
		// If we are in StateWaitFC, receiving this frame should trigger a transition.
		// So we should probably call a handler or just let the next iteration handle it?
		// In 'Run', we process Rx, then we loop back.
		// We should add a check in 'Run' or 'processTx' to see if we received FC.
		// Better: call a method that handles the state change immediately.
		t.handleTxFlowControl(f, txChan)

	case *SingleFrame:
		t.handleRxSingleFrame(f)

	case *FirstFrame:
		t.handleRxFirstFrame(f, txChan)

	case *ConsecutiveFrame:
		t.handleRxConsecutiveFrame(f, txChan)
	}
}

func (t *Transport) handleRxSingleFrame(f *SingleFrame) {
	if t.rxState != StateIdle {
		t.fireError(errors.New("警告：在多帧接收过程中被一个新单帧打断"))
	}
	t.stopReceiving()

	// Non-blocking send or drop if full?
	// For tp_layer, we usually want to ensure delivery or block.
	select {
	case t.rxDataChan <- f.Data:
	default:
		fmt.Println("Rx Buffer Full, dropping frame")
	}
}

func (t *Transport) handleRxFirstFrame(f *FirstFrame, txChan chan<- CanMessage) {
	if t.rxState != StateIdle {
		t.fireError(errors.New("警告：在多帧接收过程中被一个新首帧打断"))
	}
	t.stopReceiving()

	t.rxFrameLen = f.TotalSize
	t.rxBuffer = make([]byte, 0, f.TotalSize) // Optimize allocation
	t.rxBuffer = append(t.rxBuffer, f.Data...)

	if len(t.rxBuffer) >= t.rxFrameLen {
		select {
		case t.rxDataChan <- t.rxBuffer:
		default:
			fmt.Println("Rx Buffer Full, dropping frame")
		}
		t.stopReceiving()
	} else {
		t.rxState = StateWaitCF
		t.rxSeqNum = 1
		// Send FC (CTS)
		t.sendFlowControl(FlowStatusContinueToSend, txChan)
		t.resetRxTimer()
	}
}

func (t *Transport) handleRxConsecutiveFrame(f *ConsecutiveFrame, txChan chan<- CanMessage) {
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
			fmt.Println("Rx Buffer Full, dropping frame")
		}
		t.stopReceiving()
	} else {
		t.rxBlockCounter++
		if t.config.BlockSize > 0 && t.rxBlockCounter >= t.config.BlockSize {
			t.rxBlockCounter = 0
			t.sendFlowControl(FlowStatusContinueToSend, txChan)
			// Wait for CFs again, timer resets
			t.resetRxTimer()
		}
	}
}

func (t *Transport) resetRxTimer() {
	if !t.timerRxCF.Stop() {
		select {
		case <-t.timerRxCF.C:
		default:
		}
	}
	// Default to N_Cr timeout waiting for next CF
	t.timerRxCF.Reset(t.config.TimeoutN_Cr)
}

func (t *Transport) sendFlowControl(status FlowStatus, txChan chan<- CanMessage) {
	payload := createFlowControlPayload(status, t.config.BlockSize, t.config.StMin)
	msg := t.makeTxMsgWithAddr(t.address, payload, Physical) // FC should use the physical tester->ECU address.
	select {
	case txChan <- msg:
	default:
		// If txChan is full, we are in trouble.
	}
}
