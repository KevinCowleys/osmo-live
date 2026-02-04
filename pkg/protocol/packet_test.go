package protocol

import (
	"testing"
)

func TestPacketEncode(t *testing.T) {
	p := &Packet{
		Target:     0x0802,
		SequenceID: 0x1234,
		CmdType:    0x40,
		CmdSet:     0x02,
		CmdID:      0x01,
		Payload:    []byte{0xDE, 0xAD, 0xBE, 0xEF},
	}

	b, err := p.Encode()
	if err != nil {
		t.Fatal(err)
	}

	// 13 header/metadata + 4 payload = 17 bytes
	if len(b) != 17 {
		t.Fatalf("expected 17 bytes, got %d", len(b))
	}

	if b[0] != Magic {
		t.Errorf("magic mismatch: %x", b[0])
	}

	// Length byte (Index 1)
	if b[1] != 17 {
		t.Errorf("length field mismatch: %d", b[1])
	}
}

func TestEdgeCases(t *testing.T) {
	// Nil Payload
	p := &Packet{Payload: nil}
	b, err := p.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 13 {
		t.Errorf("want len 13 for nil payload, got %d", len(b))
	}

	// Max Payload (255 - 13 header = 242)
	p.Payload = make([]byte, 242)
	if _, err := p.Encode(); err != nil {
		t.Errorf("max length valid: %v", err)
	}

	// Too Large (243)
	p.Payload = make([]byte, 243)
	if _, err := p.Encode(); err == nil {
		t.Error("expected error for 243 byte payload")
	}
}
