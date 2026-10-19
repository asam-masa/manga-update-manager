package thumbnail

import (
	"image"
	"testing"
)

func TestValidateCover(t *testing.T) {
	valid := encodePNG(t, image.NewNRGBA(image.Rect(0, 0, 3, 4)))
	broken := append([]byte(nil), valid...)
	broken[len(broken)-1] ^= 1
	for _, tc := range []struct {
		name string
		data []byte
		ok   bool
	}{
		{"valid", valid, true},
		{"max-side", encodePNG(t, image.NewNRGBA(image.Rect(0, 0, MaxSide, MaxSide))), true},
		{"wide", encodePNG(t, image.NewNRGBA(image.Rect(0, 0, MaxSide+1, 1))), false},
		{"tall", encodePNG(t, image.NewNRGBA(image.Rect(0, 0, 1, MaxSide+1))), false},
		{"empty", nil, false},
		{"not-png", []byte("synthetic"), false},
		{"large", make([]byte, 2*1024*1024+1), false},
		{"truncated", valid[:len(valid)-5], false},
		{"crc", broken, false},
		{"trailing", append(append([]byte(nil), valid...), 0), false},
		{"animated", addPNGChunk(valid, "acTL", make([]byte, 8)), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateCover(tc.data); (err == nil) != tc.ok {
				t.Fatal(err)
			}
		})
	}
}
