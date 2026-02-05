package ble

import (
	"fmt"
	"strings"

	"tinygo.org/x/bluetooth"
)

// DjiDeviceModel is defined in models.go

type Connection struct {
	Adapter        *bluetooth.Adapter
	Device         bluetooth.Device
	Name           string
	Model          DjiDeviceModel
	ReadChar       *bluetooth.DeviceCharacteristic // fff4 (Notify)
	WriteChar      *bluetooth.DeviceCharacteristic // fff3 (Write)
	NotifyCallback func([]byte)
}

func Connect() (*Connection, error) {
	adapter := bluetooth.DefaultAdapter // Declared locally
	if err := adapter.Enable(); err != nil {
		return nil, fmt.Errorf("failed to enable BLE adapter: %w", err)
	}

	fmt.Println("Scanning for DJI devices...")

	var foundDevice bluetooth.ScanResult
	var identifiedModel DjiDeviceModel = DjiModelUnknown

	err := adapter.Scan(func(adapter *bluetooth.Adapter, result bluetooth.ScanResult) {
		// Check for DJI Service or Name or Manufacturer Data

		manufDataList := result.ManufacturerData()
		for _, md := range manufDataList {
			// ID is CompanyID (uint16)
			if md.CompanyID != 0x08AA {
				continue
			}

			id := append([]byte{0xaa, 0x08}, md.Data...)
			if model := IdentifyModel(id); model != DjiModelUnknown {
				identifiedModel = model
				foundDevice = result
				adapter.StopScan()
				return
			}
		}
	})

	if identifiedModel != DjiModelUnknown {
		fmt.Printf("Found: %s\n", identifiedModel.String())
	}

	if err != nil {
		return nil, fmt.Errorf("scan error: %w", err)
	}

	if foundDevice.Address.String() == "" {
		return nil, fmt.Errorf("no DJI device found")
	}

	fmt.Printf("Connecting to %s...\n", foundDevice.LocalName())
	device, err := adapter.Connect(foundDevice.Address, bluetooth.ConnectionParams{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	conn := &Connection{
		Adapter: adapter,
		Device:  device,
		Name:    foundDevice.LocalName(),
		Model:   identifiedModel,
	}

	// Service Discovery
	if err := conn.discoverServices(); err != nil {
		return nil, err
	}

	return conn, nil
}

// discoverServices encapsulates the service and characteristic discovery logic.
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
