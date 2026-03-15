package wsbridge

import (
	"encoding/hex"
	"testing"

	canbusdriver "github.com/LoveWonYoung/canbuskit/driver"
)

func TestEnvelopeToUnifiedConvertsFDLengthToDLC(t *testing.T) {
	payload := make([]byte, 12)
	for i := range payload {
		payload[i] = byte(i)
	}

	msg, err := envelopeToUnified(wsCANEnvelope{
		Type:     "can_frame",
		BridgeID: "bridge-a",
		Channel:  0,
		Frame: wsCANFramePayload{
			ID:      0x123,
			CANType: "CANFD",
			DLC:     uint8(len(payload)),
			DataHex: hex.EncodeToString(payload),
		},
	})
	if err != nil {
		t.Fatalf("envelopeToUnified returned error: %v", err)
	}

	if !msg.IsFD {
		t.Fatalf("expected CAN-FD message")
	}
	if msg.DLC != 9 {
		t.Fatalf("expected DLC 9 for 12-byte payload, got %d", msg.DLC)
	}
	if got := msg.Data[:12]; string(got) != string(payload) {
		t.Fatalf("payload mismatch: got %x want %x", got, payload)
	}
}

func TestFrameToEnvelopeUsesActualPayloadLength(t *testing.T) {
	cfg := Config{
		ServerURL: "ws://127.0.0.1:8898/ws",
		BridgeID:  "bridge-a",
		Side:      "right",
		Channel:   2,
	}
	dev := New(canbusdriver.CANFD, cfg)

	env := dev.frameToEnvelope(0x456, make([]byte, 16))
	if env.Direction != "right_to_left" {
		t.Fatalf("unexpected direction: %s", env.Direction)
	}
	if env.Channel != 2 {
		t.Fatalf("unexpected channel: %d", env.Channel)
	}
	if env.Frame.CANType != "CANFD" {
		t.Fatalf("unexpected can type: %s", env.Frame.CANType)
	}
	if env.Frame.DLC != 16 {
		t.Fatalf("expected websocket payload length 16, got %d", env.Frame.DLC)
	}
}

func TestNormalizeConfigRejectsMissingEndpoint(t *testing.T) {
	cfg := Config{
		BridgeID: "bridge-a",
	}
	if err := cfg.normalize(); err == nil {
		t.Fatal("expected missing server URL error")
	}
}

func TestWriteRejectsClassicCANOverflow(t *testing.T) {
	dev := New(canbusdriver.CAN, Config{})
	if err := dev.Write(0x123, make([]byte, 9)); err == nil {
		t.Fatal("expected classic CAN length error")
	}
}
