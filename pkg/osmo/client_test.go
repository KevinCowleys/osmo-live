package osmo

import (
	"testing"

	"github.com/KevinCowleys/osmo-live/pkg/ble"
	"github.com/KevinCowleys/osmo-live/pkg/commands"
)

// Mocks

type MockConn struct {
	SentData [][]byte
}

func (m *MockConn) Send(data []byte) error {
	m.SentData = append(m.SentData, data)
	return nil
}
func (m *MockConn) Subscribe(f func([]byte)) error     { return nil }
func (m *MockConn) Read() ([]byte, error)              { return nil, nil }
func (m *MockConn) Name() string                       { return "MockDevice" }
func (m *MockConn) IsModel(id ble.DjiDeviceModel) bool { return false }

// Tests

func TestCheckHeartbeat(t *testing.T) {
	c := &Client{battLevel: -1}

	// Too short
	c.checkHeartbeat([]byte{0x00})
	if c.battLevel != -1 {
		t.Fail()
	}

	// Valid (85%)
	pkt := make([]byte, 35)
	pkt[9], pkt[10] = 0x02, 0x0D // CmdSet/CmdID
	pkt[31] = 85                 // Battery
	c.checkHeartbeat(pkt)

	if c.battLevel != 85 {
		t.Errorf("want 85, got %d", c.battLevel)
	}

	// Wrong CmdID
	pkt[10] = 0x99
	pkt[31] = 90
	c.checkHeartbeat(pkt)

	if c.battLevel != 85 {
		t.Error("should ignore non-heartbeat packet")
	}
}

func TestStateMachine(t *testing.T) {
	mock := &MockConn{}
	c := &Client{
		Conn:   mock,
		State:  StateConnecting,
		Log:    &defaultLogger{},
		events: make(chan []byte, 10),
	}

	// Trigger Connecting -> Pairing
	c.stateMachine(nil)

	if c.State != StatePairing {
		t.Errorf("want StatePairing, got %v", c.State)
	}
	if len(mock.SentData) != 1 {
		t.Error("expected pairing packet to be sent")
	}

	// Simulate Pairing Response (0x92, 0x80)
	resp := make([]byte, 10)
	resp[6], resp[7] = 0x92, 0x80

	c.stateMachine(resp)
	if c.State != StateStopping {
		t.Errorf("want StateStopping, got %v", c.State)
	}
}

func TestIsSeq(t *testing.T) {
	c := &Client{}
	data := []byte{0, 0, 0, 0, 0, 0, 0x92, 0x80}

	if !c.isSeq(data, 0x92, 0x80) {
		t.Error("should match valid sequence")
	}
	if c.isSeq(data, 0x92, 0x99) {
		t.Error("should not match invalid sequence")
	}
	if c.isSeq([]byte{0}, 0x92, 0x80) {
		t.Error("should fail on short packet")
	}
}

func TestPacketConstruction(t *testing.T) {
	// Stop Packet
	p1, _ := commands.ConstructStopStreamingPacket()
	if p1.CmdID != 0x8E {
		t.Errorf("Stop: want CmdID 0x8E, got 0x%X", p1.CmdID)
	}

	// WiFi Packet
	p2, _ := commands.ConstructWiFiConnectPacket("foo", "bar")
	if p2.Payload[0] != 3 { // len("foo")
		t.Errorf("WiFi: want SSID len 3, got %d", p2.Payload[0])
	}
}
