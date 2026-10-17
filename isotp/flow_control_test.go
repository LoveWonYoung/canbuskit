package isotp

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

func TestSetDefaultFlowControlValues(t *testing.T) {
	addr, err := NewAddress(0x7C6, 0x7C7)
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.BlockSize = 8
	cfg.STMin = 15
	transport := mustNew(t, addr, cfg)

	txChan := make(chan linkFrame, 1)
	transport.sendFlowControl(FlowStatusContinueToSend, txChan)
	msg := <-txChan
	if !bytes.Equal(msg.Data, []byte{0x30, 0x08, 0x0F}) {
		t.Fatalf("unexpected flow-control payload: % X", msg.Data)
	}
}

func TestConfigValidation(t *testing.T) {
	if err := DefaultConfig().Validate(); err != nil {
		t.Fatalf("DefaultConfig().Validate() failed: %v", err)
	}
	invalid := DefaultConfig()
	invalid.ConsecutiveFrameTimeout = 0
	if err := invalid.Validate(); err == nil {
		t.Fatal("Config.Validate accepted a zero N_Cr timeout")
	}
	invalid = DefaultConfig()
	invalid.MaxPayloadSize = -1
	if err := invalid.Validate(); err == nil {
		t.Fatal("Config.Validate accepted a negative payload limit")
	}
}

func TestFlowControlSendHonorsCancellationWhenOutputBlocked(t *testing.T) {
	addr, err := NewAddress(0x7C6, 0x7C7)
	if err != nil {
		t.Fatal(err)
	}
	transport := mustNew(t, addr, DefaultConfig())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = transport.sendFlowControlContext(ctx, FlowStatusContinueToSend, make(chan linkFrame))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("sendFlowControlContext() error = %v, want context.Canceled", err)
	}
}

func TestInvalidFlowStatusStopsTransmission(t *testing.T) {
	addr, err := NewAddress(0x7C6, 0x7C7)
	if err != nil {
		t.Fatal(err)
	}
	transport := mustNew(t, addr, DefaultConfig())
	defer transport.cleanup()
	transport.txState = stateWaitFC
	transport.resetTxFCTimer()

	transport.handleTxFlowControl(&FlowControlFrame{FlowStatus: FlowStatus(3)})
	if transport.txState != stateIdle {
		t.Fatalf("txState = %v, want stateIdle", transport.txState)
	}
	select {
	case err := <-transport.Errors():
		if err == nil {
			t.Fatal("expected a non-nil invalid-flow-status error")
		}
	case <-time.After(time.Second):
		t.Fatal("invalid flow status did not report an error")
	}
}

func TestWaitFrameLimitStopsTransmission(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxWaitFrames = 1
	addr, err := NewAddress(0x7C6, 0x7C7)
	if err != nil {
		t.Fatal(err)
	}
	transport := mustNew(t, addr, cfg)
	defer transport.cleanup()
	transport.txState = stateWaitFC

	transport.handleTxFlowControl(&FlowControlFrame{FlowStatus: FlowStatusWait})
	transport.handleTxFlowControl(&FlowControlFrame{FlowStatus: FlowStatusWait})
	if transport.txState != stateIdle {
		t.Fatalf("txState = %v, want stateIdle after excessive WAIT frames", transport.txState)
	}
}

func TestFlowControlConfigRejectsInvalidInput(t *testing.T) {
	addr, err := NewAddress(0x7C6, 0x7C7)
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.BlockSize = 256
	if _, err := newSession(addr, cfg, false); err == nil {
		t.Fatal("New accepted a block size greater than one byte")
	}
	cfg = DefaultConfig()
	cfg.STMin = 128
	if _, err := newSession(addr, cfg, false); err == nil {
		t.Fatal("New accepted a reserved millisecond STmin")
	}
}

func TestManualFlowControlDisablesAutomaticSending(t *testing.T) {
	addr, err := NewAddress(0x7C6, 0x7C7)
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.ManualFlowControl = true
	transport := mustNew(t, addr, cfg)
	defer transport.cleanup()
	txChan := make(chan linkFrame, 1)
	firstFrame := linkFrame{
		ArbitrationID: addr.RxID,
		Data:          []byte{0x10, 0x0A, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
	}

	transport.processRx(context.Background(), firstFrame, txChan)
	if transport.rxState != stateWaitCF {
		t.Fatalf("transport did not enter stateWaitCF: %v", transport.rxState)
	}

	select {
	case msg := <-txChan:
		t.Fatalf("manual flow-control mode emitted an automatic frame: % X", msg.Data)
	default:
	}
}

func TestAutomaticFlowControlRemainsEnabledByDefault(t *testing.T) {
	addr, err := NewAddress(0x7C6, 0x7C7)
	if err != nil {
		t.Fatal(err)
	}
	transport := mustNew(t, addr, DefaultConfig())
	defer transport.cleanup()
	txChan := make(chan linkFrame, 1)

	transport.processRx(context.Background(), linkFrame{
		ArbitrationID: addr.RxID,
		Data:          []byte{0x10, 0x0A, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
	}, txChan)

	select {
	case msg := <-txChan:
		if !bytes.Equal(msg.Data, []byte{0x30, 0x00, 0x14}) {
			t.Fatalf("unexpected automatic flow-control payload: % X", msg.Data)
		}
	default:
		t.Fatal("automatic flow-control frame was not sent")
	}
}
