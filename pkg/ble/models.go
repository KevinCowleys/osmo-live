package ble

type DjiDeviceModel int

const (
	DjiModelUnknown DjiDeviceModel = iota
	DjiModelOsmoPocket3
	DjiModelOsmoAction3
	DjiModelOsmoAction4
	DjiModelOsmoAction5Pro
	DjiModelOsmoAction6
)

type DjiResolution uint8

const (
	Resolution1080p DjiResolution = 0x0a
	Resolution720p  DjiResolution = 0x04
	Resolution480p  DjiResolution = 0x47
)

type DjiFramerate uint8

const (
	Framerate25 DjiFramerate = 0x02
	Framerate30 DjiFramerate = 0x03
)

func (m DjiDeviceModel) String() string {
	switch m {
	case DjiModelOsmoAction3:
		return "Osmo Action 3"
	case DjiModelOsmoAction4:
		return "Osmo Action 4"
	case DjiModelOsmoAction5Pro:
		return "Osmo Action 5 Pro"
	case DjiModelOsmoAction6:
		return "Osmo Action 6"
	case DjiModelOsmoPocket3:
		return "Osmo Pocket 3"
	default:
		return "Unknown DJI Device"
	}
}

// IdentifyModel parses the Manufacturer Data from BLE advertisement
func IdentifyModel(manufData []byte) DjiDeviceModel {
	// Header: [0xaa, 0x08]
	if len(manufData) < 4 || manufData[0] != 0xaa || manufData[1] != 0x08 {
		return DjiModelUnknown
	}

	// Model ID at index 2
	switch manufData[2] {
	case 0x12:
		return DjiModelOsmoAction3
	case 0x14:
		return DjiModelOsmoAction4
	case 0x15:
		return DjiModelOsmoAction5Pro
	case 0x18:
		return DjiModelOsmoAction6
	case 0x20:
		return DjiModelOsmoPocket3
	default:
		return DjiModelUnknown
	}
}
