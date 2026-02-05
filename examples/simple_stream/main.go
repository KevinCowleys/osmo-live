package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/KevinCowleys/osmo-live/pkg/ble"
	"github.com/KevinCowleys/osmo-live/pkg/osmo"
)

func main() {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Enter WiFi SSID: ")
	ssid, _ := reader.ReadString('\n')
	ssid = strings.TrimSpace(ssid)

	fmt.Print("Enter WiFi Password: ")
	pass, _ := reader.ReadString('\n')
	pass = strings.TrimSpace(pass)

	fmt.Print("Enter RTMP URL: ")
	rtmp, _ := reader.ReadString('\n')
	rtmp = strings.TrimSpace(rtmp)

	// 1. Configure
	cfg := osmo.Config{
		SSID:     ssid,
		Password: pass,
		RTMPURL:  rtmp,
		Res:      ble.Resolution1080p,
		FPS:      ble.Framerate30,
		Bitrate:  6000,
	}

	// 1.5. Scan and Select Device (Optional)
	fmt.Println("Scanning for Osmo devices (5s)...")
	devices, err := osmo.Scan(5 * time.Second)
	if err != nil {
		log.Printf("Scan warning: %v", err)
	}

	selectedAddress := ""
	if len(devices) > 0 {
		fmt.Println("Found Devices:")
		for i, d := range devices {
			fmt.Printf("[%d] %s (%s) RSSI: %d\n", i+1, d.Name, d.Model, d.RSSI)
		}
		fmt.Print("Select device (enter number) or press Enter for auto-connect: ")
		selection, _ := reader.ReadString('\n')
		selection = strings.TrimSpace(selection)

		if idx, err := strconv.Atoi(selection); err == nil && idx > 0 && idx <= len(devices) {
			selectedAddress = devices[idx-1].Address
			fmt.Printf("Selected: %s\n", devices[idx-1].Name)
		}
	} else {
		fmt.Println("No devices found during scan. Will try auto-connect.")
	}

	// 2. Initialize Client
	client := osmo.NewClient(cfg)

	// 3. Start the connection process
	log.Printf("Connecting to %s...", func() string {
		if selectedAddress == "" {
			return "first available device"
		}
		return selectedAddress
	}())
	client.Connect(selectedAddress)

	// 4. Listen for updates and react
	for update := range client.Updates {
		switch update.Type {
		case osmo.UpdateError:
			log.Fatalf("Error: %v", update.Payload)

		case osmo.UpdateStateChange:
			state := update.Payload.(osmo.State)
			log.Printf("Current State: %v", state)

			if state != osmo.StateIdle {
				continue
			}

			log.Println("Ready. Sending start stream command...")
			if err := client.StartStream(); err != nil {
				log.Fatalf("Failed to start: %v", err)
			}
		}
	}
}
