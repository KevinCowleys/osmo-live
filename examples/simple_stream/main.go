package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"

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

	// 2. Initialize Client
	client := osmo.NewClient(cfg)

	// 3. Start the connection process
	log.Println("Connecting to Osmo...")
	client.Connect()

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
