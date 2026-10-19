package thumbnail

import (
	"bytes"
	"image/png"
)

// ValidateCover validates persisted output, without regenerating or normalizing
// it. Bound encoded bytes and dimensions before decoding untrusted PNG data.
func ValidateCover(data []byte) error {
	if len(data) == 0 || len(data) > 2*1024*1024 || !bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		return ErrInvalid
	}
	if _, err := inspectPNG(data); err != nil {
		return err
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxSide || cfg.Height > MaxSide {
		return ErrInvalid
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		return ErrInvalid
	}
	return nil
}
