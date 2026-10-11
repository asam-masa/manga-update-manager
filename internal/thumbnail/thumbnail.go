// Package thumbnail validates untrusted still images and generates PNG previews.
// It does not open paths, write files, or change database references.
package thumbnail

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"sync"

	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

const (
	MaxInputBytes = 20 * 1024 * 1024
	MaxPixels     = 20_000_000
	MaxSide       = 600
)

var (
	ErrTooLarge    = errors.New("画像は20 MiB以下にしてください")
	ErrDimensions  = errors.New("画像は2,000万画素以下にしてください")
	ErrUnsupported = errors.New("JPEG・PNG・WebPの静止画を選んでください")
	ErrAnimation   = errors.New("アニメーション画像には対応していません。静止画を選んでください")
	ErrInvalid     = errors.New("画像を読み取れません。別の画像を選んでください")
	ErrOrientation = errors.New("画像のEXIF方向情報を読み取れません。別の画像を選んでください")
	processing     sync.Mutex
)

// Generate reads at most MaxInputBytes+1 bytes and returns a complete PNG only
// after validation succeeds. Calls are serialized to bound decode memory use.
// The caller owns the reader and any later persistence; no input is modified.
func Generate(input io.Reader) ([]byte, error) {
	processing.Lock()
	defer processing.Unlock()
	data, err := io.ReadAll(io.LimitReader(input, MaxInputBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if len(data) > MaxInputBytes {
		return nil, ErrTooLarge
	}
	var config func(io.Reader) (image.Config, error)
	var decode func(io.Reader) (image.Image, error)
	var orientation int
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		config, decode = png.DecodeConfig, png.Decode
		orientation, err = inspectPNG(data)
	case bytes.HasPrefix(data, []byte{0xff, 0xd8}):
		config, decode = jpeg.DecodeConfig, jpeg.Decode
		orientation, err = inspectJPEG(data)
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		config, decode = webp.DecodeConfig, webp.Decode
		orientation, err = inspectWebP(data)
	default:
		return nil, ErrUnsupported
	}
	if err != nil {
		return nil, err
	}
	cfg, err := config(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if err := validateDimensions(cfg.Width, cfg.Height); err != nil {
		return nil, err
	}
	img, err := decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if img.Bounds().Dx() != cfg.Width || img.Bounds().Dy() != cfg.Height {
		return nil, ErrInvalid
	}
	source := orientedImage{img: img, orientation: orientation}
	w, h := source.Bounds().Dx(), source.Bounds().Dy()
	if w > MaxSide || h > MaxSide {
		if w >= h {
			h = max(1, h*MaxSide/w)
			w = MaxSide
		} else {
			w = max(1, w*MaxSide/h)
			h = MaxSide
		}
	}
	preview := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(preview, preview.Bounds(), source, source.Bounds(), draw.Src, nil)
	var result bytes.Buffer
	if err := png.Encode(&result, preview); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return result.Bytes(), nil
}

func validateDimensions(w, h int) error {
	if w <= 0 || h <= 0 || w > MaxPixels/h {
		return ErrDimensions
	}
	return nil
}

// This view applies EXIF orientation before resampling, without allocating a
// second full-sized image. Bounds are always rooted at zero.
type orientedImage struct {
	img         image.Image
	orientation int
}

func (o orientedImage) ColorModel() color.Model { return o.img.ColorModel() }
func (o orientedImage) Bounds() image.Rectangle {
	w, h := o.img.Bounds().Dx(), o.img.Bounds().Dy()
	if o.orientation >= 5 {
		w, h = h, w
	}
	return image.Rect(0, 0, w, h)
}
func (o orientedImage) At(x, y int) color.Color {
	if !image.Pt(x, y).In(o.Bounds()) {
		return color.NRGBA{}
	}
	b := o.img.Bounds()
	w, h := b.Dx(), b.Dy()
	switch o.orientation {
	case 2:
		x = w - 1 - x
	case 3:
		x, y = w-1-x, h-1-y
	case 4:
		y = h - 1 - y
	case 5:
		x, y = y, x
	case 6:
		x, y = y, h-1-x
	case 7:
		x, y = w-1-y, h-1-x
	case 8:
		x, y = w-1-y, x
	}
	return o.img.At(b.Min.X+x, b.Min.Y+y)
}
