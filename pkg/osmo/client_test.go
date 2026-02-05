package osmo

import (
	"context"
	"testing"
	"time"

	"github.com/KevinCowleys/osmo-live/pkg/ble"
	"github.com/KevinCowleys/osmo-live/pkg/commands"
)

// Mocks

type MockConn struct {
	SentData     [][]byte
	Disconnected bool
}

func (m *MockConn) Send(data []byte) error {
	m.SentData = append(m.SentData, data)
	return nil
}
func (m *MockConn) Subscribe(f func([]byte)) error     { return nil }
func (m *MockConn) Read() ([]byte, error)              { return nil, nil }
func (m *MockConn) Name() string                       { return "MockDevice" }
func (m *MockConn) IsModel(id ble.DjiDeviceModel) bool { return false }
func (m *MockConn) Disconnect() error {
	m.Disconnected = true
	return nil
}

// helper to create a client with channel buffers suitable for testing
func newTestClient(mock *MockConn) *Client {
	return &Client{
		Conn:      mock,
		State:     StateIdle,
		Log:       &defaultLogger{},
		events:    make(chan []byte, 10),
		Updates:   make(chan Update, 10),
		done:      make(chan struct{}),
		battLevel: -1,
	}
}

// Tests

func TestPacketKeepAlive(t *testing.T) {
	c := &Client{
		battLevel: -1,
		Updates:   make(chan Update, 10),
		Log:       &defaultLogger{},
	}

	// Set last heartbeat to past
	c.lastHeartbeat = time.Now().Add(-1 * time.Minute)
	oldTime := c.lastHeartbeat

	// Handle random packet
	c.handlePacket([]byte{0x01, 0x02, 0x03})

	// Should have updated time
	if !c.lastHeartbeat.After(oldTime) {
		t.Error("expected lastHeartbeat to update on packet receipt")
	}

	// Should be recent (within 1s)
	if time.Since(c.lastHeartbeat) > time.Second {
		t.Error("lastHeartbeat is not recent")
	}
}

func TestBatteryParsing(t *testing.T) {
	c := &Client{
		battLevel: -1,
		Updates:   make(chan Update, 10),
		Log:       &defaultLogger{},
	}

	// 1. Valid Packet
	// CmdSet 0x0D, CmdID 0x02, Offset 31 = Battery
	pkt := make([]byte, 40)
	pkt[9] = 0x0D  // CmdSet
	pkt[10] = 0x02 // CmdID
	pkt[31] = 95   // Battery Level = 95%

	c.checkBattery(pkt)

	if c.BatteryLevel() != 95 {
		t.Errorf("valid: expected 95, got %d", c.BatteryLevel())
	}

	// Verify update
	select {
	case u := <-c.Updates:
		if u.Payload.(int) != 95 {
			t.Errorf("valid: wrong update payload: %v", u.Payload)
		}
	default:
		t.Error("valid: missing update")
	}

	// 2. Negative Test: Wrong CmdID
	pkt[10] = 0x99
	pkt[31] = 100 // Try to change battery to 100
	c.checkBattery(pkt)

	if c.BatteryLevel() != 95 {
		t.Errorf("negative(cmd): should stay 95, got %d", c.BatteryLevel())
	}

	// Ensure no update sent
	select {
	case <-c.Updates:
		t.Error("negative(cmd): should not send update")
	default:
	}

	// 3. Negative Test: Too Short
	shortPkt := make([]byte, 30) // Need > 31
	shortPkt[9] = 0x0D
	shortPkt[10] = 0x02
	c.checkBattery(shortPkt)

	if c.BatteryLevel() != 95 {
		t.Errorf("negative(len): should stay 95, got %d", c.BatteryLevel())
	}
}

func TestStateMachine(t *testing.T) {
	mock := &MockConn{}
	c := &Client{
		Conn:    mock,
		State:   StateConnecting,
		Log:     &defaultLogger{},
		events:  make(chan []byte, 10),
		Updates: make(chan Update, 10),
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

	// Should now be idle
	if c.State != StateIdle {
		t.Errorf("expected StateIdle, got %v", c.State)
	}
}

func TestIsSeq(t *testing.T) {
	c := &Client{}

	// Valid sequence check
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

func TestStreamControl(t *testing.T) {
	mock := &MockConn{}
	client := newTestClient(mock)

	// Verify StartStream flow
	err := client.StartStream()
	if err != nil {
		t.Fatalf("StartStream failed: %v", err)
	}

	// Should transition to StateResetting as the first step
	if client.State != StateResetting {
		t.Errorf("expected state StateResetting, got %v", client.State)
	}

	// Verify that a cleanup packet (StopStreaming) was sent first
	if len(mock.SentData) == 0 {
		t.Fatal("expected cleanup packet to be sent")
	}
	pktSent := mock.SentData[0]
	if pktSent[10] != 0x8E { // cmdID for Stop
		t.Errorf("expected Stop packet (CmdID 0x8E), got 0x%X", pktSent[10])
	}

	// Verify StopStream flow
	// Manually set state to Streaming to simulate active stream
	client.State = StateStreaming
	mock.SentData = nil

	err = client.StopStream()
	if err != nil {
		t.Fatalf("StopStream failed: %v", err)
	}

	if client.State != StateStopping {
		t.Errorf("expected state Stopping, got %v", client.State)
	}

	// Verify stop command sent
	if len(mock.SentData) == 0 {
		t.Fatal("expected stop command to be sent")
	}
	if mock.SentData[0][10] != 0x8E {
		t.Errorf("expected Stop packet (CmdID 0x8E), got 0x%X", mock.SentData[0][10])
	}

	// Receive Stop Ack
	ack := make([]byte, 10)
	ack[6], ack[7] = 0xc8, 0xea

	client.handlePacket(ack)

	if client.State != StateIdle {
		t.Errorf("expected state Idle after Ack, got %v", client.State)
	}
}

func TestDisconnect(t *testing.T) {
	mock := &MockConn{}
	client := newTestClient(mock)

	// Ensure background loop signal is open
	select {
	case <-client.done:
		t.Error("done channel should be open initially")
	default:
		// good
	}

	err := client.Disconnect()
	if err != nil {
		t.Fatalf("Disconnect failed: %v", err)
	}

	// Verify background loop signal is closed
	select {
	case <-client.done:
		// good, channel is closed
	case <-time.After(100 * time.Millisecond):
		t.Error("done channel was not closed")
	}

	// Verify underlying connection was disconnected
	if !mock.Disconnected {
		t.Error("ble connection was not disconnected")
	}
}

func TestScan(t *testing.T) {
	originalScan := bleScan
	defer func() { bleScan = originalScan }()

	mockDevices := []ble.ScannedDevice{
		{Name: "Osmo Action 4", Address: "11:22:33:44:55:66", Model: ble.DjiModelOsmoAction4, RSSI: -50},
	}

	bleScan = func(ctx context.Context) ([]ble.ScannedDevice, error) {
		return mockDevices, nil
	}

	devices, err := Scan(1 * time.Second)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(devices) != 1 {
		t.Fatalf("Expected 1 device, got %d", len(devices))
	}

	d := devices[0]
	if d.Name != "Osmo Action 4" || d.Address != "11:22:33:44:55:66" || d.RSSI != -50 {
		t.Errorf("Device mismatch: %+v", d)
	}
}

func TestConnectSelection(t *testing.T) {
	mock := &MockConn{}
	client := newTestClient(mock)

	wantedAddress := "AA:BB:CC:DD:EE:FF"
	connectedAddr := ""

	client.Connector = func(address string) (Conn, error) {
		connectedAddr = address
		return mock, nil
	}

	client.Connect(wantedAddress)
	time.Sleep(50 * time.Millisecond)

	if connectedAddr != wantedAddress {
		t.Errorf("Connect called with wrong address: want %s, got %s", wantedAddress, connectedAddr)
	}

	// Verify it proceeds to connect phase
	if client.Conn != mock {
		t.Error("Client Conn not set correctly")
	}
}

func TestConnectCancellation(t *testing.T) {
	mock := &MockConn{Disconnected: false}

	client := &Client{
		Log:       &defaultLogger{},
		Connector: nil,
		Updates:   make(chan Update, 10),
		done:      make(chan struct{}),
	}

	// Simulate a slow connection process
	connBlocked := make(chan struct{})
	client.Connector = func(address string) (Conn, error) {
		<-connBlocked // Wait until we signal to proceed
		return mock, nil
	}

	client.Connect("")

	// Immediately disconnect
	client.Disconnect()

	// Allow connection to complete
	close(connBlocked)
	time.Sleep(50 * time.Millisecond)

	if !mock.Disconnected {
		t.Error("Connection should have been disconnected immediately due to race condition check")
	}

	if client.Conn != nil {
		t.Error("Client Conn should not have been set")
	}
}
