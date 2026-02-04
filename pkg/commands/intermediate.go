package commands

import (
	"osmo-live/pkg/protocol"
)

// ConstructStopStreamingPacket sends 0x8E command to stop
// Target: 0x0802
func ConstructStopStreamingPacket() (*protocol.Packet, error) {
	return &protocol.Packet{
		Target:     0x0802,
		SequenceID: 0xeac8,
		CmdType:    0x40,
		CmdSet:     0x02,
		CmdID:      0x8E,
		Payload:    []byte{0x01, 0x01, 0x1a, 0x00, 0x01, 0x02},
	}, nil
}

// ConstructPreparingPacket sends 0xE1 command
// Target: 0x0802
func ConstructPreparingPacket() (*protocol.Packet, error) {
	return &protocol.Packet{
		Target:     0x0802,
		SequenceID: 0x8c12,
		CmdType:    0x40,
		CmdSet:     0x02,
		CmdID:      0xE1,
		Payload:    []byte{0x1a},
	}, nil
}

// ConstructConfigurePacket sets stabilization mode
// Target: 0x0102
func ConstructConfigurePacket(mode int) (*protocol.Packet, error) {
	// Modes: 0=Off, 1=RS, 2=HS, 3=RS+, 4=HB
	return &protocol.Packet{
		Target:     0x0102,
		SequenceID: 0x8c2d,
		CmdType:    0x40,
		CmdSet:     0x02,
		CmdID:      0x8E,
		Payload:    []byte{0x01, 0x01, 0x08, 0x00, 0x01, uint8(mode)},
	}, nil
}
