package osmo

import (
	"fmt"
	"log"
	"time"

	"osmo-live/pkg/ble"
	"osmo-live/pkg/commands"
)

// State tracks the connection phase
type State int

const (
	StateConnecting State = iota
	StatePairing
	StateStopping
	StatePreparing
	StateWiFi
	StateConfiguring
	StateStartingRTMP
	StateStreaming
)

// Config holds all parameters for the stream
type Config struct {
	SSID     string
	Password string
	RTMPURL  string
	Res      ble.DjiResolution
	FPS      ble.DjiFramerate
	Bitrate  int
	Steady   int
}

type Conn interface {
	Send(data []byte) error
	Subscribe(func([]byte)) error
	Read() ([]byte, error)
	Name() string
	IsModel(ble.DjiDeviceModel) bool
}

// Logger allows redirecting library output (e.g. for TUI/GUI)
type Logger interface {
	Printf(format string, v ...interface{})
	Println(v ...interface{})
}

// defaultLogger uses standard fmt package
type defaultLogger struct{}

func (l *defaultLogger) Printf(format string, v ...interface{}) { fmt.Printf(format, v...) }
func (l *defaultLogger) Println(v ...interface{})               { fmt.Println(v...) }

// Client represents a controller for a DJI Osmo device
type Client struct {
	Config Config
	Conn   Conn // Use interface
	State  State
	Log    Logger // Optional logger

	// Internal
	events        chan []byte
	lastHeartbeat time.Time
	battLevel     int

	// Retry state
	lastPacket []byte
	lastTxTime time.Time
	attempts   int
}

// BleWrapper adapts ble.Connection to osmo.Conn
type BleWrapper struct {
	*ble.Connection
}

const (
	maxRetries = 3
	txTimeout  = 2 * time.Second
)

// NewClient creates a new Osmo controller
func NewClient(cfg Config) *Client {
	return &Client{
		Config:    cfg,
		State:     StateConnecting,
		Log:       &defaultLogger{},
		events:    make(chan []byte, 10),
		battLevel: -1,
	}
}

// Connect Scans and connects to the device
func (c *Client) Connect() error {
	conn, err := ble.Connect()
	if err != nil {
		return err
	}
	c.Conn = &BleWrapper{conn}
	c.Log.Printf("Connected to %s\n", conn.Name)
	return nil
}

func (w *BleWrapper) Name() string {
	return w.Connection.Name
}

func (w *BleWrapper) IsModel(m ble.DjiDeviceModel) bool {
	return w.Connection.Model == m
}

// Start begins the main event loop
func (c *Client) Start() error {
	if c.Conn == nil {
		if err := c.Connect(); err != nil {
			return err
		}
	}

	if err := c.subscribe(); err != nil {
		log.Printf("sub error: %v", err)
	}

	c.readInitial()
	c.lastHeartbeat = time.Now()

	for {
		select {
		case data := <-c.events:
			c.handlePacket(data)
		case <-time.After(500 * time.Millisecond):
			if err := c.handleTick(); err != nil {
				return err
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
	c.checkHeartbeat(data)
	c.stateMachine(data)
}

func (c *Client) checkHeartbeat(data []byte) {
	if len(data) <= 10 || data[9] != 0x02 || data[10] != 0x0D {
		return
	}

	c.lastHeartbeat = time.Now()
	if len(data) < 32 {
		return
	}

	lvl := int(data[31])
	if lvl != c.battLevel {
		c.battLevel = lvl
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
		c.Log.Println("> Paired. Stopping...")
		pkt, _ := commands.ConstructStopStreamingPacket()
		b, _ := pkt.Encode()
		c.sendValid(b)
		c.State = StateStopping

	case StateStopping:
		if !c.isSeq(data, 0xc8, 0xea) {
			return
		}
		c.reset()
		c.Log.Println("> Stopped. Preparing...")
		pkt, _ := commands.ConstructPreparingPacket()
		b, _ := pkt.Encode()
		c.sendValid(b)
		c.State = StatePreparing

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

	case StateWiFi:
		if !c.isSeq(data, 0x19, 0x8c) {
			return
		}
		c.reset()
		c.Log.Println("> WiFi OK.")

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
	c.Log.Printf("\n! Timeout. Retry %d/%d... ", c.attempts, maxRetries)
	c.Conn.Send(c.lastPacket)
	c.lastTxTime = time.Now()

	return nil
}

func (c *Client) monitorStream() {
	if c.State != StateStreaming {
		return
	}

	if time.Since(c.lastHeartbeat) >= 5*time.Second {
		c.Log.Println("\n! No Heartbeat (5s)")
		return
	}

	bs := "??"
	if c.battLevel >= 0 {
		bs = fmt.Sprintf("%d%%", c.battLevel)
	}
	c.Log.Printf("\r> Streaming | Batt: %s | Last HB: %s   ", bs, time.Since(c.lastHeartbeat).Round(time.Second))
}

// Stop sends a stop streaming command
func (c *Client) Stop() error {
	c.Log.Println("\n> Stopping stream...")
	pkt, _ := commands.ConstructStopStreamingPacket()
	b, _ := pkt.Encode()
	return c.Conn.Send(b)
}

// Helpers

func (c *Client) sendValid(data []byte) {
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
}

func (c *Client) isSeq(data []byte, b0, b1 byte) bool {
	if len(data) >= 8 {
		return data[6] == b0 && data[7] == b1
	}
	return false
}
