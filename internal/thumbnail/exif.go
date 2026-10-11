package thumbnail

import (
	"bytes"
	"encoding/binary"
)

// Only the TIFF IFD0 orientation is interpreted. Other metadata is discarded,
// not copied into the PNG. No offsets are dereferenced before bounds checks.
func exifOrientation(data []byte) (int, error) {
	data = bytes.TrimPrefix(data, []byte("Exif\x00\x00"))
	if len(data) < 8 {
		return 0, ErrOrientation
	}
	var order binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0, ErrOrientation
	}
	if order.Uint16(data[2:4]) != 42 {
		return 0, ErrOrientation
	}
	offset := uint64(order.Uint32(data[4:8]))
	if offset < 8 || offset+2 > uint64(len(data)) {
		return 0, ErrOrientation
	}
	count := uint64(order.Uint16(data[offset : offset+2]))
	if offset+2+count*12+4 > uint64(len(data)) {
		return 0, ErrOrientation
	}
	orientation, found := 1, false
	for i := uint64(0); i < count; i++ {
		entry := data[offset+2+i*12 : offset+2+(i+1)*12]
		if order.Uint16(entry[:2]) != 0x0112 {
			continue
		}
		if found || order.Uint16(entry[2:4]) != 3 || order.Uint32(entry[4:8]) != 1 {
			return 0, ErrOrientation
		}
		orientation = int(order.Uint16(entry[8:10]))
		if orientation < 1 || orientation > 8 {
			return 0, ErrOrientation
		}
		found = true
	}
	return orientation, nil
}
