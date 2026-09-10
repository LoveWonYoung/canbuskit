package tp_layer

import (
	"bytes"
	"testing"
)

func TestSetDefaultFlowControlValues(t *testing.T) {
	addr, err := NewAddress(0x7C6, 0x7C7)
	if err != nil {
		t.Fatal(err)
	}
	transport := NewTransport(addr, DefaultConfig())

	if err := transport.SetDefaultBlockSize(8); err != nil {
		t.Fatalf("SetDefaultBlockSize() failed: %v", err)
	}
	if err := transport.SetDefaultStMin(15); err != nil {
		t.Fatalf("SetDefaultStMin() failed: %v", err)
	}

	txChan := make(chan CanMessage, 1)
	transport.sendFlowControl(FlowStatusContinueToSend, txChan)
	msg := <-txChan
	if !bytes.Equal(msg.Data, []byte{0x30, 0x08, 0x0F}) {
		t.Fatalf("unexpected flow-control payload: % X", msg.Data)
	}
}

func TestSetDefaultFlowControlValuesRejectInvalidInput(t *testing.T) {
	addr, err := NewAddress(0x7C6, 0x7C7)
	if err != nil {
		t.Fatal(err)
	}
	transport := NewTransport(addr, DefaultConfig())

	if err := transport.SetDefaultBlockSize(256); err == nil {
		t.Fatal("SetDefaultBlockSize() accepted a value greater than one byte")
	}
	if err := transport.SetDefaultStMin(128); err == nil {
		t.Fatal("SetDefaultStMin() accepted a reserved millisecond value")
	}

	blockSize, stMin := transport.flowControlDefaults()
	if blockSize != 0 || stMin != 20 {
		t.Fatalf("invalid setters changed defaults: blockSize=%d stMin=%d", blockSize, stMin)
	}
}

func TestManualFlowControlDisablesAutomaticSending(t *testing.T) {
	addr, err := NewAddress(0x7C6, 0x7C7)
	if err != nil {
		t.Fatal(err)
	}
	transport := NewTransport(addr, DefaultConfig())
	defer transport.cleanup()
	txChan := make(chan CanMessage, 1)
	firstFrame := CanMessage{
		ArbitrationID: addr.RxID,
		Data:          []byte{0x10, 0x0A, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
	}

	transport.SetManualFlowControl(true)
	transport.ProcessRx(firstFrame, txChan)
	if transport.rxState != StateWaitCF {
		t.Fatalf("transport did not enter StateWaitCF: %v", transport.rxState)
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
	transport := NewTransport(addr, DefaultConfig())
	defer transport.cleanup()
	txChan := make(chan CanMessage, 1)

	transport.ProcessRx(CanMessage{
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
