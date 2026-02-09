package tp_layer

import (
	"errors"
	"fmt"
	"time"
)

// ProcessTx is removed in favor of event-driven handlers called from Run()

// initiateTx starts the transmission of a new message.
// It is called when data arrives on txDataChan and state is Idle.
func (t *Transport) initiateTx(payload []byte, txChan chan<- CanMessage) {
	t.txBuffer = payload
	t.txFrameLen = len(payload)

	// 判断是单帧还是多帧
	sfPciSize := 1
	if t.txFrameLen > 7 {
		sfPciSize = 2
	}

	if t.txFrameLen+sfPciSize <= t.MaxDataLength {
		// 作为单帧发送
		data, err := createSingleFramePayload(payload, t.MaxDataLength)
		if err != nil {
			t.fireError(fmt.Errorf("Error creating SF: %v", err))
			t.stopSending()
			return
		}

		msg := t.makeTxMsg(data, Physical)
		select {
		case txChan <- msg:
			// Sent successfully
		default:
			t.fireError(errors.New("Tx Channel full, dropping SF"))
		}
		// Done
		t.stopSending() // Resets state to Idle

	} else {
		// 作为多帧发送，先发送首帧
		ffPciSize := 2
		if t.txFrameLen > 4095 {
			ffPciSize = 6
		}
		chunkSize := t.MaxDataLength - ffPciSize

		// Take first chunk for FF
		firstChunk := t.txBuffer[:chunkSize]
		t.txBuffer = t.txBuffer[chunkSize:]

		data, err := createFirstFramePayload(firstChunk, t.txFrameLen, t.MaxDataLength)
		if err != nil {
			t.fireError(fmt.Errorf("Error creating FF: %v", err))
			t.stopSending()
			return
		}

		t.txSeqNum = 1
		t.txState = StateWaitFC

		msg := t.makeTxMsg(data, Physical)
		select {
		case txChan <- msg:
		default:
			t.fireError(errors.New("Tx Channel full, dropping FF"))
			t.stopSending()
			return
		}

		// Start FC timeout timer
		t.resetTxFCTimer()
	}
}

func (t *Transport) handleTxFlowControl(fc *FlowControlFrame, txChan chan<- CanMessage) {
	if t.txState != StateWaitFC {
		// We might receive FC when we are not waiting for it (e.g. unsolicited or late).
		// Just ignore.
		return
	}

	t.timerRxFC.Stop()
	t.timerTxSTmin.Stop()

	switch fc.FlowStatus {
	case FlowStatusContinueToSend:
		t.wftCounter = 0
		t.remoteBlocksize = fc.BlockSize

		// Set STmin
		t.remoteStmin = fc.STmin

		t.txState = StateTransmit
		t.txBlockCounter = 0

		// Start STmin timer to trigger first CF send.
		// If STmin is 0, we could send immediately, but using the timer loop is cleaner
		// and prevents blocking the RX loop for too long.
		t.resetTxSTminTimer(fc.STmin)

		// Check WFT
		// Uses a hardcoded limit for now as it's not in standard config usually, or add to config?
		// Let's use a safe default constant.
		const MaxWaitFrames = 20
		if t.wftCounter > MaxWaitFrames {
			t.fireError(errors.New("错误：等待帧(Wait Frame)数量超出最大限制"))
			t.stopSending()
		} else {
			t.resetTxFCTimer()
		}

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

	chunkSize := t.MaxDataLength - 1 // CF PCI=1
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

	msg := t.makeTxMsg(data, Physical)
	select {
	case txChan <- msg:
	default:
		t.fireError(errors.New("Tx Channel full, dropping CF"))
		// If we drop a CF, the transfer is likely corrupted. Abort?
		t.stopSending()
		return
	}

	if len(t.txBuffer) == 0 {
		// Transfer finished
		// fmt.Println("多帧数据发送完成。")
		t.stopSending()
		return
	}

	// Determine next step
	if t.remoteBlocksize > 0 && t.txBlockCounter >= t.remoteBlocksize {
		// Block finished, wait for FC
		t.txState = StateWaitFC
		t.resetTxFCTimer()
	} else {
		// Continue sending after STmin
		// Use the stored stmin value (we parsed it from FC)
		t.resetTxSTminTimer(t.remoteStmin)
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
