package tp_layer

import (
	"errors"
	"fmt"
)

// ProcessRx Modified to take txChan to allow sending FlowControl frames directly
func (t *Transport) ProcessRx(msg CanMessage, txChan chan<- CanMessage) {
	if !t.address.IsForMe(&msg) {
		return
	}
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
	t.timerRxCF.Reset(t.config.TimeoutN_Cr)
}

func (t *Transport) sendFlowControl(status FlowStatus, txChan chan<- CanMessage) {
	payload := createFlowControlPayload(status, t.config.BlockSize, t.config.StMin)
	msg := t.makeTxMsgWithAddr(t.address, payload, Physical) // FC should use the physical tester->ECU address.
	select {
	case txChan <- msg:
	default:
	}
}
