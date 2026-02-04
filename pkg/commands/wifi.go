package commands

import (
	"github.com/KevinCowleys/osmo-live/pkg/ble"
	"github.com/KevinCowleys/osmo-live/pkg/protocol"
)

var seqID uint16 = 0x1000

func getSeqID() uint16 {
	seqID++
	return seqID
}

// Updated Wifi Packet for node-osmo protocol
// ConstructWiFiConnectPacket sends WiFi credentials
func ConstructWiFiConnectPacket(ssid, password string) (*protocol.Packet, error) {
	payload := []byte{uint8(len(ssid))}
	payload = append(payload, []byte(ssid)...)

	payload = append(payload, uint8(len(password)))
	payload = append(payload, []byte(password)...)

	return &protocol.Packet{
		Target:     0x0702,
		SequenceID: 0x8c19,
		CmdType:    0x40,
		CmdSet:     0x07,
		CmdID:      0x47,
		Payload:    payload,
	}, nil
}

// ConstructRTMPStartPacket builds the start command
func ConstructRTMPStartPacket(url string, res ble.DjiResolution, fps ble.DjiFramerate, bitrate int) (*protocol.Packet, error) {
	// DjiResolution and DjiFramerate map directly to the protocol bytes
	resByte := uint8(res)
	fpsByte := uint8(fps)

	if bitrate == 0 {
		bitrate = 6000
	}
	bitrateLE := []byte{uint8(bitrate & 0xff), uint8((bitrate >> 8) & 0xff)}

	// RTMP Payload Structure
	// [0x00, 0x2e, 0x00, Res, Bitrate_L, Bitrate_H, 0x02, 0x00, FPS, 0x00, 0x00, 0x00, URL_Len, 0x00, URL...]

	payload := []byte{0x00, 0x2e, 0x00, resByte}
	payload = append(payload, bitrateLE...)
	payload = append(payload, []byte{0x02, 0x00, fpsByte, 0x00, 0x00, 0x00}...)

	payload = append(payload, uint8(len(url)), 0)
	payload = append(payload, []byte(url)...)

	return &protocol.Packet{
		Target:     0x0802,
		SequenceID: 0x8c2c,
		CmdType:    0x40,
		CmdSet:     0x08,
		CmdID:      0x78,
		Payload:    payload,
	}, nil
}
