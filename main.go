package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"osmo-live/pkg/ble"
	"osmo-live/pkg/osmo"
)

func main() {
	ssid := flag.String("ssid", "", "WiFi SSID")
	password := flag.String("password", "", "WiFi Password")
	rtmpURL := flag.String("rtmp", "", "RTMP Server URL")

	res := flag.Int("res", 1080, "Resolution (720, 1080)")
	fps := flag.Int("fps", 30, "Framerate (25, 30)")
	bitrate := flag.Int("bitrate", 6000, "Bitrate in Kbps")
	steady := flag.Int("steady", 1, "Stabilization: 0=Off, 1=RS, 2=HS, 3=RS+, 4=HB")

	flag.Parse()

	if *ssid == "" || *password == "" || *rtmpURL == "" {
		log.Fatal("All flags (-ssid, -password, -rtmp) are required")
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
		if client.Conn != nil {
			if err := client.Stop(); err != nil {
				fmt.Printf("Error sending stop command: %v\n", err)
			} else {
				fmt.Println(">> Stop command sent.")
			}
			time.Sleep(500 * time.Millisecond)
		}
		os.Exit(0)
	}()

	fmt.Println("Starting DJI Streamer (Library Mode)...")
	if err := client.Start(); err != nil {
		log.Fatalf("Streamer Error: %v", err)
	}
}
