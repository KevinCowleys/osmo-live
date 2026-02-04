# DJI Osmo RTMP Streamer (Go)

A simple Go tool to stream from your DJI Osmo straight to RTMP. No app required.

Should work with:
- Osmo Action 3 / 4 / 5 Pro / 6
- Osmo Pocket 3

Personally tested with:
- Osmo Action 4

## Usage

### CLI

You'll need `sudo` for Bluetooth access.

```bash
# Build
go build -o dji_streamer main.go

# Run
sudo ./dji_streamer \
  -ssid "MyWiFi" \
  -password "MyPassword" \
  -rtmp "rtmp://live.twitch.tv/app/..."
```

### As a Library

Want to build your own tool? Check out the [examples/](examples/) folder.

**Simple Example:** [`examples/simple_stream/main.go`](examples/simple_stream/main.go)

```go
package main

import (
    "github.com/KevinCowleys/osmo-live/pkg/ble"
    "github.com/KevinCowleys/osmo-live/pkg/osmo"
)

func main() {
    client := osmo.NewClient(osmo.Config{
        SSID:     "MyWiFi",
        Password: "MyPassword",
        RTMPURL:  "rtmp://...",
        Res:      ble.Resolution1080p,
        FPS:      ble.Framerate30,
    })

    if err := client.Start(); err != nil {
        log.Fatal(err)
    }
}
```

## Flags

| Flag | Default | Notes |
|---|---|---|
| `-ssid` | | **Required** |
| `-password` | | **Required** |
| `-rtmp` | | **Required** |
| `-res` | 1080 | 720, 1080 |
| `-fps` | 30 | 25, 30 |
| `-bitrate` | 6000 | Kbps |
| `-steady` | 1 | 0=Off, 1=RS, 2=HS (Action 4/5 only) |

## Credits

Big thanks to the **[node-osmo](https://github.com/Start-Streaming/node-osmo)** project for reverse engineering the protocol. This is basically a Go port of their hard work.
