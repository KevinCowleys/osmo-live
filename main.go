package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/KevinCowleys/osmo-live/pkg/ble"
	"github.com/KevinCowleys/osmo-live/pkg/osmo"
)

func main() {
	ssid := flag.String("ssid", "", "WiFi SSID")
	password := flag.String("password", "", "WiFi Password")
	rtmpURL := flag.String("rtmp", "", "RTMP Server URL")

	res := flag.Int("res", 1080, "Resolution (480, 720, 1080)")
	fps := flag.Int("fps", 30, "Framerate (25, 30)")
	bitrate := flag.Int("bitrate", 6000, "Bitrate in Kbps")
	steady := flag.Int("steady", 1, "Stabilization: 0=Off, 1=RS, 2=HS, 3=RS+, 4=HB")
	connectOnly := flag.Bool("connect-only", false, "Connect only, do not start stream")

	deviceAddr := flag.String("device", "", "Specific BLE Address to connect to")
	doScan := flag.Bool("scan", false, "Scan for devices and exit")

	flag.Parse()

	if *ssid == "" || *password == "" || *rtmpURL == "" {
		log.Fatal("All flags (-ssid, -password, -rtmp) are required")
	}

	if *doScan {
		fmt.Println("Scanning for Osmo devices (5s)...")
		devices, err := osmo.Scan(5 * time.Second)
		if err != nil {
			log.Fatalf("Scan error: %v", err)
		}

		if len(devices) == 0 {
			fmt.Println("No devices found.")
			os.Exit(0)
		}

		fmt.Println("Found Devices:")
		for i, d := range devices {
			fmt.Printf("[%d] %s (RSSI: %d) %s\n", i+1, d.Name, d.RSSI, d.Address)
		}

		fmt.Print("\nSelect device # to connect (or 0 to exit): ")
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = input[:len(input)-1] // trim newline

		idx, err := strconv.Atoi(input)
		if err != nil || idx < 1 || idx > len(devices) {
			fmt.Println("Invalid selection or exit.")
			os.Exit(0)
		}

		*deviceAddr = devices[idx-1].Address
		fmt.Printf("Selected: %s\n", devices[idx-1].Name)
	}

	var djiRes ble.DjiResolution
	switch *res {
	case 1080:
		djiRes = ble.Resolution1080p
	case 720:
		djiRes = ble.Resolution720p
	case 480:
		djiRes = ble.Resolution480p
	default:
		djiRes = ble.Resolution1080p
	}

	var djiFps ble.DjiFramerate
	switch *fps {
	case 25:
		djiFps = ble.Framerate25
	case 30:
		djiFps = ble.Framerate30
	default:
		djiFps = ble.Framerate30
	}

	config := osmo.Config{
		SSID:     *ssid,
		Password: *password,
		RTMPURL:  *rtmpURL,
		Res:      djiRes,
		FPS:      djiFps,
		Bitrate:  *bitrate,
		Steady:   *steady,
	}

	client := osmo.NewClient(config)

	// Graceful Shutdown Handler
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		fmt.Println("\n\n>> Interrupt received.")
		client.StopStream()
		client.Disconnect()
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}()

	fmt.Println("Starting DJI Streamer (Library Mode)...")

	client.Connect(*deviceAddr)
	fmt.Println("Connecting...")

	// Input Handler (for interactive start/stop)
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			text := scanner.Text()
			switch text {
			case "", "start":
				// Only allow manual start if we are in Connect-Only mode or just idle
				if client.State != osmo.StateIdle {
					fmt.Println(">> Waiting for Idle state...")
					continue
				}

				fmt.Println(">> Manual Start Requested...")
				if err := client.StartStream(); err != nil {
					log.Printf("Failed to start stream: %v", err)
				}

			case "stop":
				fmt.Println(">> Stopping and Exiting...")
				client.StopStream()
				client.Disconnect()
				time.Sleep(500 * time.Millisecond) // Give time for cleanup packets
				os.Exit(0)
			}
		}
	}()

	// State Machine Loop
	isIdle := false
	for update := range client.Updates {
		switch update.Type {
		case osmo.UpdateStateChange:
			state := update.Payload.(osmo.State)
			fmt.Printf("State: %v\n", state)

			// Once we're paired and idle, we can start the stream
			if state != osmo.StateIdle || isIdle {
				continue
			}

			isIdle = true

			if *connectOnly {
				fmt.Println("Device ready. Idle mode (Connect Only).")
				fmt.Println(">> Press [ENTER] to start stream, or type 'stop' to stop.")
				continue
			}

			fmt.Println("Device ready. Starting stream...")
			if err := client.StartStream(); err != nil {
				log.Printf("Failed to start stream: %v", err)
			}

		case osmo.UpdateError:
			fmt.Printf("Error: %v\n", update.Payload)
		}
	}
}
