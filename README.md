# DJI Osmo RTMP Streamer

A simple Go tool to stream from your DJI Osmo straight to RTMP. No app required.

Should work with:
- Osmo Action 3 / 4 / 5 Pro / 6
- Osmo Pocket 3

Personally tested with:
- Osmo Action 4

## Usage

### CLI

You'll need `sudo` for Bluetooth access.

This will connect to the first device that gets found and start streaming.

```bash
# Build
go build -o osmo-live main.go

# Run
sudo ./osmo-live \
  -ssid "MyWiFi" \
  -password "MyPassword" \
  -rtmp "rtmp://..."
```

**Interactive Connect (Scan & Select):**

This will scan for devices and allow you to select one to connect to.

```bash
sudo ./osmo-live \
  -scan \
  -ssid "MyWiFi" \
  -password "MyPassword" \
  -rtmp "rtmp://..."
```

### As a Library

Want to build your own tool? Check out the [examples/](examples/) folder.

**Simple Example:** [`examples/simple_stream/main.go`](examples/simple_stream/main.go)


## Flags

| Flag | Default | Notes |
|---|---|---|
| `-ssid` | | **Required** |
| `-password` | | **Required** |
| `-rtmp` | | **Required** |
| `-res` | 1080 | 480, 720, 1080 |
| `-fps` | 30 | 25, 30 |
| `-bitrate` | 6000 | Kbps |
| `-steady` | 1 | 0=Off, 1=RS, 2=HS (Action 4/5 only) |
| `-connect-only` | false | Connect and idle. Press Enter to start stream. |
| `-scan` | false | List devices. Use with other flags to select & connect interactively. |
| `-device` | | Connect to specific BLE Address (e.g. `AA:BB:CC...`). |

## Credits

Big thanks to the **[node-osmo](https://github.com/datagutt/node-osmo)** project for reverse engineering the protocol. This is basically a Go port of their hard work.
