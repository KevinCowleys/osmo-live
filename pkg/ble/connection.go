package ble

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"tinygo.org/x/bluetooth"
)

// DjiDeviceModel is defined in models.go

var (
	scanCache = make(map[string]bluetooth.ScanResult)
	cacheMu   sync.Mutex
)

type ScannedDevice struct {
	Name    string
	Address string
	Model   DjiDeviceModel
	RSSI    int16
}

type Connection struct {
	Adapter        *bluetooth.Adapter
	Device         bluetooth.Device
	Name           string
	Model          DjiDeviceModel
	ReadChar       *bluetooth.DeviceCharacteristic // fff4 (Notify)
	WriteChar      *bluetooth.DeviceCharacteristic // fff3 (Write)
	NotifyCallback func([]byte)
}

// Scan for DJI devices until context timeout.
// Caches results to speed up subsequent connections.
func Scan(ctx context.Context) ([]ScannedDevice, error) {
	adapter := bluetooth.DefaultAdapter
	if err := adapter.Enable(); err != nil {
		return nil, fmt.Errorf("failed to enable BLE adapter: %w", err)
	}

	fmt.Println("Scanning for DJI devices...")

	var devices []ScannedDevice
	seen := make(map[string]bool)

	// TinyGo's scan is blocking, so use a background routine for timeout
	done := make(chan struct{})

	go func() {
		select {
		case <-ctx.Done():
			adapter.StopScan()
		case <-done:
			// Scan finished naturally
		}
	}()

	err := adapter.Scan(func(adapter *bluetooth.Adapter, result bluetooth.ScanResult) {
		// Update cache regardless of whether we've seen it in this specific scan session
		cacheMu.Lock()
		scanCache[result.Address.String()] = result
		cacheMu.Unlock()

		if seen[result.Address.String()] {
			return
		}

		manufDataList := result.ManufacturerData()
		for _, md := range manufDataList {
			if md.CompanyID != 0x08AA {
				continue
			}

			id := append([]byte{0xaa, 0x08}, md.Data...)
			if model := IdentifyModel(id); model != DjiModelUnknown {
				seen[result.Address.String()] = true
				devices = append(devices, ScannedDevice{
					Name:    result.LocalName(),
					Address: result.Address.String(),
					Model:   model,
					RSSI:    result.RSSI,
				})
				return
			}
		}
	})

	close(done)

	if err != nil && ctx.Err() == nil {
		return nil, fmt.Errorf("scan error: %w", err)
	}

	return devices, nil
}

func Connect(address string) (*Connection, error) {
	adapter := bluetooth.DefaultAdapter
	if err := adapter.Enable(); err != nil {
		return nil, fmt.Errorf("failed to enable BLE adapter: %w", err)
	}

	var deviceToConnect bluetooth.ScanResult
	found := false

	if address != "" {
		cacheMu.Lock()
		cached, ok := scanCache[address]
		cacheMu.Unlock()
		if ok {
			fmt.Printf("Device %s found in cache. Connecting...\n", address)
			deviceToConnect = cached
			found = true
		}
	}

	if !found {
		fmt.Println("Device not in cache (or auto-connect). Scanning...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		scanErr := adapter.Scan(func(adapter *bluetooth.Adapter, result bluetooth.ScanResult) {
			cacheMu.Lock()
			scanCache[result.Address.String()] = result
			cacheMu.Unlock()

			if address != "" && result.Address.String() != address {
				return
			}

			manufDataList := result.ManufacturerData()
			for _, md := range manufDataList {
				if md.CompanyID != 0x08AA {
					continue
				}
				id := append([]byte{0xaa, 0x08}, md.Data...)
				if IdentifyModel(id) != DjiModelUnknown {
					deviceToConnect = result
					found = true
					adapter.StopScan()
					cancel()
					return
				}
			}
		})

		// Make sure we wait for the scan to actually stop if we cancelled it
		<-ctx.Done()

		if scanErr != nil && scanErr.Error() != "interrupted" && ctx.Err() != context.Canceled {
			fmt.Printf("Scan warning: %v\n", scanErr)
		}
	}

	if !found {
		return nil, fmt.Errorf("device not found (address: %s)", address)
	}

	// Double check the model if we can
	var identifiedModel DjiDeviceModel = DjiModelUnknown
	for _, md := range deviceToConnect.ManufacturerData() {
		if md.CompanyID == 0x08AA {
			id := append([]byte{0xaa, 0x08}, md.Data...)
			identifiedModel = IdentifyModel(id)
			break
		}
	}

	fmt.Printf("Connecting to %s...\n", deviceToConnect.LocalName())
	device, err := adapter.Connect(deviceToConnect.Address, bluetooth.ConnectionParams{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	conn := &Connection{
		Adapter: adapter,
		Device:  device,
		Name:    deviceToConnect.LocalName(),
		Model:   identifiedModel,
	}

	// Service Discovery
	if err := conn.discoverServices(); err != nil {
		conn.Disconnect()
		return nil, err
	}

	return conn, nil
}

func (c *Connection) discoverServices() error {
	fmt.Println("Discovering services...")
	srvs, err := c.Device.DiscoverServices(nil)
	if err != nil {
		return fmt.Errorf("failed to discover services: %w", err)
	}

	// Find fff0 Service -> fff3 (Write) & fff4 (Notify)
	for _, srv := range srvs {
		if !strings.Contains(srv.UUID().String(), "fff0") {
			continue
		}

		chars, err := srv.DiscoverCharacteristics(nil)
		if err != nil {
			continue
		}

		for _, char := range chars {
			uuid := char.UUID().String()
			if strings.Contains(uuid, "fff3") {
				cCopy := char
				c.WriteChar = &cCopy
			} else if strings.Contains(uuid, "fff4") {
				cCopy := char
				c.ReadChar = &cCopy
			}
		}
	}

	if c.WriteChar == nil || c.WriteChar.UUID().String() == "" {
		return fmt.Errorf("could not find write characteristic (fff3)")
	}

	if c.ReadChar == nil || c.ReadChar.UUID().String() == "" {
		return fmt.Errorf("could not find notify characteristic (fff4)")
	}

	return nil
}

func (c *Connection) Send(data []byte) error {
	_, err := c.WriteChar.WriteWithoutResponse(data)
	return err
}

func (c *Connection) Subscribe(callback func([]byte)) error {
	c.NotifyCallback = callback
	return c.ReadChar.EnableNotifications(func(buf []byte) {
		if c.NotifyCallback != nil {
			data := make([]byte, len(buf))
			copy(data, buf)
			c.NotifyCallback(data)
		}
	})
}

func (c *Connection) Read() ([]byte, error) {
	buf := make([]byte, 255)
	n, err := c.ReadChar.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

func (c *Connection) Disconnect() error {
	return c.Device.Disconnect()
}
