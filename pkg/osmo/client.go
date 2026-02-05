package osmo

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/KevinCowleys/osmo-live/pkg/ble"
	"github.com/KevinCowleys/osmo-live/pkg/commands"
)

var bleScan = ble.Scan

// Connection phase
type State int

const (
	StateConnecting State = iota
	StatePairing
	StateIdle
	StateResetting
	StateStopping
	StatePreparing
	StateWiFi
	StateConfiguring
	StateStartingRTMP
	StateStreaming
)

// Stream parameters
type Config struct {
	SSID     string
	Password string
	RTMPURL  string
	Res      ble.DjiResolution
	FPS      ble.DjiFramerate
	Bitrate  int
	Steady   int
}

type DiscoveredDevice struct {
	Name    string
	Address string
	Model   string
	RSSI    int16
}

// Scan for DJI devices.
// Context timeout controls how long we listen.
func Scan(duration time.Duration) ([]DiscoveredDevice, error) {
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	results, err := bleScan(ctx)
	if err != nil {
		return nil, err
	}

	var devices []DiscoveredDevice
	for _, r := range results {
		devices = append(devices, DiscoveredDevice{
			Name:    r.Name,
			Address: r.Address,
			Model:   r.Model.String(),
			RSSI:    r.RSSI,
		})
	}
	return devices, nil
}

type Conn interface {
	Send(data []byte) error
	Subscribe(func([]byte)) error
	Read() ([]byte, error)
	Name() string
	IsModel(ble.DjiDeviceModel) bool
	Disconnect() error
}

type Connector func(address string) (Conn, error)

// Logger allows redirecting library output (e.g. for TUI/GUI)
type Logger interface {
	Printf(format string, v ...interface{})
	Println(v ...interface{})
}

// defaultLogger uses standard fmt package
type defaultLogger struct{}

func (l *defaultLogger) Printf(format string, v ...interface{}) { fmt.Printf(format, v...) }
func (l *defaultLogger) Println(v ...interface{})               { fmt.Println(v...) }

type UpdateType int

const (
	UpdateBattery UpdateType = iota
	UpdateStateChange
	UpdateError
)

// Event from the client
type Update struct {
	Type    UpdateType
	Payload interface{} // int for battery, State for state change, error for error
}

// DJI Osmo device
type Client struct {
	Config Config
	Conn   Conn // Use interface
	State  State
	Log    Logger // Optional logger

	Connector Connector // Function to establish connection

	// Public Updates channel
	Updates chan Update

	// Internal
	events        chan []byte
	done          chan struct{}
	lastHeartbeat time.Time
	battLevel     int

	// Retry state
	lastPacket []byte
	lastTxTime time.Time
	attempts   int
}

type BleWrapper struct {
	*ble.Connection
}

const (
	maxRetries = 3
	// Action 4 is reportedly slow to switch modes, keep this generous.
	// We increased this to 5s because 2s was triggering false positives during startup.
	txTimeout = 5 * time.Second
)

func NewClient(cfg Config) *Client {
	return &Client{
		Config:    cfg,
		State:     StateConnecting,
		Log:       &defaultLogger{},
		Connector: defaultConnector,
		Updates:   make(chan Update, 50),
		events:    make(chan []byte, 50),
		done:      make(chan struct{}),
		battLevel: -1,
	}
}

func defaultConnector(address string) (Conn, error) {
	c, err := ble.Connect(address)
	if err != nil {
		return nil, err
	}
	return &BleWrapper{c}, nil
}

// pushUpdate sends an update in a non-blocking way to ensure the event loop never freezes
func (c *Client) pushUpdate(u Update) {
	select {
	case c.Updates <- u:
	default:
		c.Log.Printf("Warning: Updates channel full. Dropping update type %v", u.Type)
	}
}

// Connect starts the connection process to a specific device.
// If address is empty, it will fallback to "first found" (backward compatibility).
func (c *Client) Connect(address string) {
	go func() {
		// Empty address = auto scan/first found
		conn, err := c.Connector(address)
		if err != nil {
			c.pushUpdate(Update{Type: UpdateError, Payload: fmt.Errorf("ble connect: %w", err)})
			return
		}

		// Race fix: user might have mashed Ctrl+C while we were connecting
		select {
		case <-c.done:
			c.Log.Println("Connect aborted.")
			conn.Disconnect()
			return
		default:
		}

		c.Conn = conn
		c.Log.Printf("Connected to %s\n", conn.Name())

		// Sync channel to make sure we're subscribed before sending packets
		ready := make(chan struct{})
		go c.run(ready)
		<-ready

		// Start the handshake sequence
		c.Log.Println("Starting pairing...")
		pkt, _ := commands.ConstructPairingPacket()
		b, _ := pkt.Encode()

		c.sendValid(b)

		c.State = StatePairing
		c.pushUpdate(Update{Type: UpdateStateChange, Payload: StatePairing})
	}()
}

func (w *BleWrapper) Name() string {
	return w.Connection.Name
}

func (w *BleWrapper) IsModel(m ble.DjiDeviceModel) bool {
	return w.Connection.Model == m
}

func (w *BleWrapper) Disconnect() error {
	return w.Connection.Disconnect()
}

// Background loop
func (c *Client) run(ready chan struct{}) {
	if err := c.subscribe(); err != nil {
		log.Printf("sub error: %v", err)
		if ready != nil {
			close(ready) // unblock caller even if we failed
		}
		return
	}

	// Signal we are listening
	if ready != nil {
		close(ready)
	}

	c.readInitial()
	c.lastHeartbeat = time.Now()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case data := <-c.events:
			c.handlePacket(data)
		case <-ticker.C:
			if err := c.handleTick(); err != nil {
				c.Log.Printf("Tick error: %v", err)
			}
		}
	}
}

func (c *Client) subscribe() error {
	return c.Conn.Subscribe(func(data []byte) {
		c.events <- data
	})
}

func (c *Client) readInitial() {
	if initData, err := c.Conn.Read(); err == nil && len(initData) > 0 {
		c.events <- initData
	} else {
		c.events <- []byte{}
	}
}

func (c *Client) handlePacket(data []byte) {
	c.lastHeartbeat = time.Now()
	c.checkBattery(data)
	c.stateMachine(data)
}

func (c *Client) checkBattery(data []byte) {
	// DJI "Push Info" packet (CmdSet 0x0D, CmdID 0x02)
	// This packet contains device status including battery.
	if len(data) < 13 || data[9] != 0x0D || data[10] != 0x02 {
		return
	}

	// Battery level is located at payload index 20.
	// Header is 11 bytes, so absolute index is 31.
	const batteryIdx = 31
	if len(data) <= batteryIdx {
		return
	}

	lvl := int(data[batteryIdx])
	if lvl != c.battLevel {
		c.battLevel = lvl
		c.pushUpdate(Update{Type: UpdateBattery, Payload: lvl})
	}
}

func (c *Client) stateMachine(data []byte) {
	switch c.State {
	case StateConnecting:
		c.Log.Println("> Pairing...")
		pkt, _ := commands.ConstructPairingPacket()
		b, _ := pkt.Encode()
		c.sendValid(b)
		c.State = StatePairing

	case StatePairing:
		if !c.isSeq(data, 0x92, 0x80) {
			return
		}
		c.reset()
		c.Log.Println("Paired. Device is idle.")
		c.State = StateIdle
		c.pushUpdate(Update{Type: UpdateStateChange, Payload: StateIdle})

	case StateResetting:
		if !c.isSeq(data, 0xc8, 0xea) {
			return
		}
		c.reset()
		c.Log.Println("> Reset. Preparing...")
		pkt, _ := commands.ConstructPreparingPacket()
		b, _ := pkt.Encode()
		c.sendValid(b)
		c.State = StatePreparing
		c.pushUpdate(Update{Type: UpdateStateChange, Payload: StatePreparing})

	case StateStopping:
		// Expect same ack as StateResetting since command is identical
		if !c.isSeq(data, 0xc8, 0xea) {
			return
		}
		c.reset()
		c.Log.Println("> Stopped. Idle.")
		c.State = StateIdle
		c.pushUpdate(Update{Type: UpdateStateChange, Payload: StateIdle})

	case StatePreparing:
		if !c.isSeq(data, 0x12, 0x8c) {
			return
		}
		c.reset()
		c.Log.Println("> Prepared. Sending WiFi...")
		pkt, _ := commands.ConstructWiFiConnectPacket(c.Config.SSID, c.Config.Password)
		b, _ := pkt.Encode()
		c.sendValid(b)
		c.State = StateWiFi
		c.pushUpdate(Update{Type: UpdateStateChange, Payload: StateWiFi})

	case StateWiFi:
		if !c.isSeq(data, 0x19, 0x8c) {
			return
		}
		c.reset()
		c.Log.Println("> WiFi Connected.")

		// Branch based on Model
		if c.Conn.IsModel(ble.DjiModelOsmoAction3) || c.Conn.IsModel(ble.DjiModelOsmoPocket3) {
			c.Log.Println("> Skipping config. Starting RTMP...")
			c.startRTMP()
			return
		}

		c.Log.Println("> Configuring...")
		pkt, _ := commands.ConstructConfigurePacket(c.Config.Steady)
		b, _ := pkt.Encode()
		c.sendValid(b)
		c.State = StateConfiguring
		c.pushUpdate(Update{Type: UpdateStateChange, Payload: StateConfiguring})

	case StateConfiguring:
		if !c.isSeq(data, 0x2d, 0x8c) {
			return
		}
		c.reset()
		c.Log.Println("> Configured. Starting RTMP...")
		c.startRTMP()

	case StateStartingRTMP:
		if !c.isSeq(data, 0x2c, 0x8c) {
			return
		}
		c.reset()
		c.Log.Println("> Stream Requested. Monitoring.")
		c.State = StateStreaming
		c.pushUpdate(Update{Type: UpdateStateChange, Payload: StateStreaming})
	}
}

func (c *Client) handleTick() error {
	if err := c.checkRetry(); err != nil {
		return err
	}
	c.monitorStream()
	return nil
}

func (c *Client) checkRetry() error {
	if c.lastPacket == nil || c.State == StateStreaming {
		return nil
	}

	if time.Since(c.lastTxTime) <= txTimeout {
		return nil
	}

	if c.attempts >= maxRetries {
		return fmt.Errorf("timeout: device didn't respond")
	}

	c.attempts++
	c.Log.Printf("> No response yet... Retry %d/%d... \n", c.attempts, maxRetries)
	c.Conn.Send(c.lastPacket)
	c.lastTxTime = time.Now()

	return nil
}

func (c *Client) monitorStream() {
	if c.State != StateStreaming {
		return
	}

	hbAge := time.Since(c.lastHeartbeat)
	warn := ""
	if hbAge >= 5*time.Second {
		warn = " (NO PACKETS)"
	}

	bs := "??"
	if c.battLevel >= 0 {
		bs = fmt.Sprintf("%d%%", c.battLevel)
	}
	c.Log.Printf("\r> Streaming | Batt: %s | Last Rx: %s%s   ", bs, hbAge.Round(time.Second), warn)
}

// Configure updates the flight parameters (only when Idle)
func (c *Client) Configure(cfg Config) error {
	if c.State != StateIdle {
		return fmt.Errorf("cannot configure while not idle (current: %v)", c.State)
	}
	c.Config = cfg
	return nil
}

// StartStream begins the streaming sequence
func (c *Client) StartStream() error {
	if c.State != StateIdle {
		return fmt.Errorf("cannot start stream while not idle (current: %v)", c.State)
	}

	c.Log.Println("Starting stream sequence...")

	// Always cleanup/stop first to be safe
	pkt, _ := commands.ConstructStopStreamingPacket()
	b, _ := pkt.Encode()
	c.sendValid(b)
	c.State = StateResetting
	c.pushUpdate(Update{Type: UpdateStateChange, Payload: StateResetting})
	return nil
}

// StopStream sends stop command and returns to Idle
func (c *Client) StopStream() error {
	c.Log.Println("\n> Stopping stream...")
	pkt, _ := commands.ConstructStopStreamingPacket()
	b, _ := pkt.Encode()
	c.sendValid(b)
	c.State = StateStopping
	c.pushUpdate(Update{Type: UpdateStateChange, Payload: StateStopping})
	return nil
}

// BatteryLevel returns the current battery percentage (0-100) or -1 if unknown
func (c *Client) BatteryLevel() int {
	return c.battLevel
}

// Disconnect closes the connection and cleans up
func (c *Client) Disconnect() error {
	c.Log.Println("> Disconnecting...")

	// Signal background loop to stop
	select {
	case <-c.done:
		// already closed
	default:
		close(c.done)
	}

	if c.Conn != nil {
		return c.Conn.Disconnect()
	}
	return nil
}

// Helpers

func (c *Client) sendValid(data []byte) {
	if c.Conn == nil {
		return
	}
	c.lastPacket = data
	c.lastTxTime = time.Now()
	c.attempts = 0
	c.Conn.Send(data)
}

func (c *Client) reset() {
	c.lastPacket = nil
}

func (c *Client) startRTMP() {
	pkt, _ := commands.ConstructRTMPStartPacket(c.Config.RTMPURL, c.Config.Res, c.Config.FPS, c.Config.Bitrate)
	b, _ := pkt.Encode()
	c.sendValid(b)
	c.State = StateStartingRTMP
	c.pushUpdate(Update{Type: UpdateStateChange, Payload: StateStartingRTMP})
}

func (c *Client) isSeq(data []byte, b0, b1 byte) bool {
	if len(data) >= 8 {
		return data[6] == b0 && data[7] == b1
	}
	return false
}
