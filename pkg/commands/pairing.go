package commands

import (
	"github.com/KevinCowleys/osmo-live/pkg/protocol"
)

// ConstructPairingPacket sends the initial handshake
// Target: 0x0702
func ConstructPairingPacket() (*protocol.Packet, error) {
	// Fixed pairing key string
	payload := []byte{0x20}
	payload = append(payload, []byte("284ae5b8d76b3375a04a6417ad71bea3")...)

	// Default pin code "love"
	payload = append(payload, 0x04)
	payload = append(payload, []byte("love")...)

	return &protocol.Packet{
		Target:     0x0702,
		SequenceID: 0x8092,
		CmdType:    0x40,
		CmdSet:     0x07,
		CmdID:      0x45,
		Payload:    payload,
	}, nil
}
