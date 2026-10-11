package thumbnail

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"io"

	"golang.org/x/image/webp"
)

func inspectPNG(data []byte) (int, error) {
	orientation, seenEXIF := 1, false
	for pos := 8; pos < len(data); {
		if len(data)-pos < 12 {
			return 0, ErrInvalid
		}
		size := uint64(binary.BigEndian.Uint32(data[pos : pos+4]))
		if size > uint64(len(data)-pos-12) {
			return 0, ErrInvalid
		}
		end := pos + 8 + int(size)
		if crc32.ChecksumIEEE(data[pos+4:end]) != binary.BigEndian.Uint32(data[end:end+4]) {
			return 0, ErrInvalid
		}
		switch string(data[pos+4 : pos+8]) {
		case "acTL", "fcTL", "fdAT":
			return 0, ErrAnimation
		case "eXIf":
			if seenEXIF {
				return 0, ErrOrientation
			}
			var err error
			orientation, err = exifOrientation(data[pos+8 : end])
			if err != nil {
				return 0, err
			}
			seenEXIF = true
		case "IEND":
			if size != 0 || end+4 != len(data) {
				return 0, ErrInvalid
			}
			return orientation, nil
		}
		pos = end + 4
	}
	return 0, ErrInvalid
}

func inspectWebP(data []byte) (int, error) {
	if uint64(binary.LittleEndian.Uint32(data[4:8]))+8 != uint64(len(data)) {
		return 0, ErrInvalid
	}
	orientation, seenEXIF, bitstreams, seenExtended := 1, false, 0, false
	canvasW, canvasH, frameW, frameH := 0, 0, 0, 0
	for pos := 12; pos < len(data); {
		if len(data)-pos < 8 {
			return 0, ErrInvalid
		}
		size := uint64(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		padded := size + size%2
		if padded > uint64(len(data)-pos-8) {
			return 0, ErrInvalid
		}
		end := pos + 8 + int(size)
		if size%2 != 0 && data[end] != 0 {
			return 0, ErrInvalid
		}
		switch string(data[pos : pos+4]) {
		case "ANIM", "ANMF":
			return 0, ErrAnimation
		case "VP8X":
			if seenExtended || pos != 12 || size != 10 {
				return 0, ErrInvalid
			}
			seenExtended = true
			if data[pos+8]&2 != 0 {
				return 0, ErrAnimation
			}
			if data[pos+8]&0xc1 != 0 || !bytes.Equal(data[pos+9:pos+12], []byte{0, 0, 0}) {
				return 0, ErrInvalid
			}
			w, h := uint24(data[pos+12:pos+15])+1, uint24(data[pos+15:pos+18])+1
			if err := validateDimensions(w, h); err != nil {
				return 0, err
			}
			canvasW, canvasH = w, h
		case "VP8 ", "VP8L":
			bitstreams++
			// VP8X dimensions alone are not sufficient: a hidden bitstream can
			// declare a larger allocation. Inspect its independent header too.
			header := make([]byte, 12)
			copy(header, "RIFF")
			copy(header[8:], "WEBP")
			binary.LittleEndian.PutUint32(header[4:8], uint32(4+8+padded))
			cfg, err := webp.DecodeConfig(io.MultiReader(bytes.NewReader(header), bytes.NewReader(data[pos:pos+8+int(padded)])))
			if err != nil {
				return 0, ErrInvalid
			}
			if err := validateDimensions(cfg.Width, cfg.Height); err != nil {
				return 0, err
			}
			frameW, frameH = cfg.Width, cfg.Height
		case "EXIF":
			if seenEXIF {
				return 0, ErrOrientation
			}
			var err error
			orientation, err = exifOrientation(data[pos+8 : end])
			if err != nil {
				return 0, err
			}
			seenEXIF = true
		}
		pos += 8 + int(padded)
	}
	if bitstreams != 1 {
		return 0, ErrInvalid
	}
	if seenExtended && (canvasW != frameW || canvasH != frameH) {
		return 0, ErrInvalid
	}
	return orientation, nil
}

func uint24(b []byte) int { return int(b[0]) | int(b[1])<<8 | int(b[2])<<16 }

func inspectJPEG(data []byte) (int, error) {
	if len(data) < 4 || !bytes.Equal(data[len(data)-2:], []byte{0xff, 0xd9}) {
		return 0, ErrInvalid
	}
	orientation, seenEXIF, entropy := 1, false, false
	for pos := 2; pos < len(data); {
		if entropy {
			for pos < len(data) && data[pos] != 0xff {
				pos++
			}
		}
		if pos >= len(data) {
			return 0, ErrInvalid
		}
		if data[pos] != 0xff {
			return 0, ErrInvalid
		}
		for pos < len(data) && data[pos] == 0xff {
			pos++
		}
		if pos >= len(data) {
			return 0, ErrInvalid
		}
		marker := data[pos]
		pos++
		if entropy && (marker == 0x00 || marker >= 0xd0 && marker <= 0xd7) {
			continue
		}
		entropy = false
		if marker == 0xd9 {
			if pos != len(data) {
				return 0, ErrInvalid
			}
			return orientation, nil
		}
		if marker == 0x00 || marker == 0xd8 || marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
			return 0, ErrInvalid
		}
		if len(data)-pos < 2 {
			return 0, ErrInvalid
		}
		size := int(binary.BigEndian.Uint16(data[pos : pos+2]))
		if size < 2 || size > len(data)-pos {
			return 0, ErrInvalid
		}
		segment := data[pos+2 : pos+size]
		if marker == 0xe1 && bytes.HasPrefix(segment, []byte("Exif\x00\x00")) {
			if seenEXIF {
				return 0, ErrOrientation
			}
			var err error
			orientation, err = exifOrientation(segment)
			if err != nil {
				return 0, err
			}
			seenEXIF = true
		}
		pos += size
		if marker == 0xda {
			entropy = true
		}
	}
	return 0, ErrInvalid
}
