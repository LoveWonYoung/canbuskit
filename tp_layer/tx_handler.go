package tp_layer

import (
	"errors"
	"fmt"
	"time"
)

// initiateTx starts the transmission of a new message.
// It is called when data arrives on txDataChan and state is Idle.
func (t *Transport) initiateTx(payload []byte, txChan chan<- CanMessage) {
	t.txBuffer = payload
	t.txFrameLen = len(payload)
	maxDataLength := t.maxDataLength()

	// 判断是单帧还是多帧
	sfPciSize := 1
	if t.txFrameLen > 7 {
		sfPciSize = 2
	}

	if t.txFrameLen+sfPciSize <= maxDataLength {
		// 作为单帧发送
		data, err := createSingleFramePayload(payload, maxDataLength)
		if err != nil {
			t.fireError(fmt.Errorf("Error creating SF: %v", err))
			t.stopSending()
			return
		}

		msg := t.makeTxMsg(data)
		// 阻塞发送：对端 STmin=0 时会高速产生 CF，非阻塞入队在 TX 消费略慢时会丢帧并破坏多帧语义。
		txChan <- msg
		// Done
		t.stopSending() // Resets state to Idle

	} else {
		// 作为多帧发送，先发送首帧
		ffPciSize := 2
		if t.txFrameLen > 4095 {
			ffPciSize = 6
		}
		chunkSize := maxDataLength - ffPciSize

		// Take first chunk for FF
		firstChunk := t.txBuffer[:chunkSize]
		t.txBuffer = t.txBuffer[chunkSize:]

		data, err := createFirstFramePayload(firstChunk, t.txFrameLen, maxDataLength)
		if err != nil {
			t.fireError(fmt.Errorf("Error creating FF: %v", err))
			t.stopSending()
			return
		}

		t.txSeqNum = 1
		t.txState = StateWaitFC

		msg := t.makeTxMsg(data)
		txChan <- msg

		// Start FC timeout timer
		t.resetTxFCTimer()
	}
}

func (t *Transport) handleTxFlowControl(fc *FlowControlFrame) {
	if t.txState != StateWaitFC {
		// We might receive FC when we are not waiting for it (e.g. unsolicited or late).
		// Just ignore.
		return
	}

	t.timerRxFC.Stop()
	t.timerTxSTmin.Stop()

	switch fc.FlowStatus {
	case FlowStatusContinueToSend:
		t.remoteBlockSize = fc.BlockSize
		t.remoteSTmin = fc.STmin
		t.txState = StateTransmit
		t.txBlockCounter = 0
		t.resetTxSTminTimer(fc.STmin)

	case FlowStatusWait:
		t.resetTxFCTimer()

	case FlowStatusOverflow:
		t.fireError(errors.New("错误：对方缓冲区溢出，停止发送"))
		t.stopSending()
	}
}

// handleTxTransmit sends the next Consecutive Frame.
// It is called when STmin timer expires.
func (t *Transport) handleTxTransmit(txChan chan<- CanMessage) {
	if len(t.txBuffer) == 0 {
		t.stopSending()
		return
	}

	chunkSize := t.maxDataLength() - 1 // CF PCI=1
	var chunk []byte
	if len(t.txBuffer) > chunkSize {
		chunk = t.txBuffer[:chunkSize]
		t.txBuffer = t.txBuffer[chunkSize:]
	} else {
		chunk = t.txBuffer
		t.txBuffer = nil
	}

	data, err := createConsecutiveFramePayload(chunk, t.txSeqNum)
	if err != nil {
		t.fireError(fmt.Errorf("Error creating CF: %v", err))
		t.stopSending()
		return
	}

	t.txSeqNum = (t.txSeqNum + 1) % 16
	t.txBlockCounter++

	msg := t.makeTxMsg(data)
	txChan <- msg

	if len(t.txBuffer) == 0 {
		// Transfer finished
		// fmt.Println("多帧数据发送完成。")
		t.stopSending()
		return
	}

	// Determine next step
	if t.remoteBlockSize > 0 && t.txBlockCounter >= t.remoteBlockSize {
		// Block finished, wait for FC
		t.txState = StateWaitFC
		t.resetTxFCTimer()
	} else {
		// Continue sending after STmin
		// Use the stored stmin value (we parsed it from FC)
		t.resetTxSTminTimer(t.remoteSTmin)
	}
}

func (t *Transport) resetTxFCTimer() {
	if !t.timerRxFC.Stop() {
		select {
		case <-t.timerRxFC.C:
		default:
		}
	}
	t.timerRxFC.Reset(t.config.TimeoutN_Bs) // N_Bs timeout
}

func (t *Transport) resetTxSTminTimer(d time.Duration) {
	if !t.timerTxSTmin.Stop() {
		select {
		case <-t.timerTxSTmin.C:
		default:
		}
	}
	t.timerTxSTmin.Reset(d)
}
