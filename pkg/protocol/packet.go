package protocol

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// Packet represents a DJI DUML packet
type Packet struct {
	Magic   uint8 // 0x55
	Length  uint8
	Version uint8 // 0x04
	// Header CRC8
	Target     uint16
	SequenceID uint16
	CmdType    uint8
	CmdSet     uint8
	CmdID      uint8
	Payload    []byte
	// Body CRC16
}

const (
	Magic   = 0x55
	Version = 0x04
)

// CRC Tables
var (
	crc8Table  = make([]uint8, 256)
	crc16Table = make([]uint16, 256)
)

func init() {
	// CRC8 (Poly: 0x31, Init: 0xEE, Reflected)
	poly8 := uint8(0x8C)
	for i := 0; i < 256; i++ {
		crc := uint8(i)
		for j := 0; j < 8; j++ {
			if (crc & 1) != 0 {
				crc = (crc >> 1) ^ poly8
			} else {
				crc >>= 1
			}
		}
		crc8Table[i] = crc
	}

	// CRC16 (Poly: 0x1021, Init: 0x496c, Reflected)
	poly16 := uint16(0x8408)
	for i := 0; i < 256; i++ {
		crc := uint16(i)
		for j := 0; j < 8; j++ {
			if (crc & 1) != 0 {
				crc = (crc >> 1) ^ poly16
			} else {
				crc >>= 1
			}
		}
		crc16Table[i] = crc
	}
}

func CalculateCRC8(data []byte) uint8 {
	crc := uint8(0x77)
	for _, b := range data {
		crc = crc8Table[crc^b]
	}
	return crc
}

func CalculateCRC16(data []byte) uint16 {
	crc := uint16(0x3692)
	for _, b := range data {
		crc = (crc >> 8) ^ crc16Table[byte(crc)^b]
	}
	return crc
}

// Encode serializes the packet
func (p *Packet) Encode() ([]byte, error) {
	totalLen := 13 + len(p.Payload)
	if totalLen > 255 {
		return nil, fmt.Errorf("packet too long")
	}

	buf := new(bytes.Buffer)

	// Header (Magic + Len + Ver)
	buf.WriteByte(Magic)
	buf.WriteByte(uint8(totalLen))
	buf.WriteByte(Version)

	// CRC8 of Header
	headerCRC := CalculateCRC8(buf.Bytes())
	buf.WriteByte(headerCRC)

	// Body
	binary.Write(buf, binary.LittleEndian, p.Target)
	binary.Write(buf, binary.LittleEndian, p.SequenceID)
	buf.WriteByte(p.CmdType)
	buf.WriteByte(p.CmdSet)
	buf.WriteByte(p.CmdID)
	buf.Write(p.Payload)

	// CRC16
	binary.Write(buf, binary.LittleEndian, CalculateCRC16(buf.Bytes()))

	return buf.Bytes(), nil
}
