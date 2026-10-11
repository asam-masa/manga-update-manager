package thumbnail

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"sync"
	"testing"
)

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func generate(t *testing.T, data []byte) image.Image {
	t.Helper()
	out, err := Generate(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	return img
}
func pngChunk(kind string, data []byte) []byte {
	b := make([]byte, 12+len(data))
	binary.BigEndian.PutUint32(b[:4], uint32(len(data)))
	copy(b[4:], kind)
	copy(b[8:], data)
	binary.BigEndian.PutUint32(b[len(b)-4:], crc32.ChecksumIEEE(b[4:len(b)-4]))
	return b
}
func addPNGChunk(data []byte, kind string, payload []byte) []byte {
	b := append([]byte{}, data[:33]...)
	b = append(b, pngChunk(kind, payload)...)
	return append(b, data[33:]...)
}
func tiffOrientation(value uint16, order binary.ByteOrder) []byte {
	b := make([]byte, 26)
	if order == binary.LittleEndian {
		copy(b, "II")
	} else {
		copy(b, "MM")
	}
	order.PutUint16(b[2:], 42)
	order.PutUint32(b[4:], 8)
	order.PutUint16(b[8:], 1)
	order.PutUint16(b[10:], 0x0112)
	order.PutUint16(b[12:], 3)
	order.PutUint32(b[14:], 1)
	order.PutUint16(b[18:], value)
	return b
}
func riffChunk(kind string, data []byte) []byte {
	b := make([]byte, 8+len(data)+len(data)%2)
	copy(b, kind)
	binary.LittleEndian.PutUint32(b[4:], uint32(len(data)))
	copy(b[8:], data)
	return b
}
func riff(chunks ...[]byte) []byte {
	b := []byte("RIFF\x00\x00\x00\x00WEBP")
	for _, chunk := range chunks {
		b = append(b, chunk...)
	}
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	return b
}

// Synthetic VP8L: one constant symbol per Huffman tree. No external image or
// encoder is required. Bits are written least-significant first per WebP spec.
func losslessWebP(w, h int, alpha byte) []byte {
	var stream []byte
	bit := 0
	put := func(v uint32, n int) {
		for i := 0; i < n; i++ {
			if bit%8 == 0 {
				stream = append(stream, 0)
			}
			stream[bit/8] |= byte((v>>i)&1) << uint(bit%8)
			bit++
		}
	}
	put(0x2f, 8)
	put(uint32(w-1), 14)
	put(uint32(h-1), 14)
	put(1, 1)
	put(0, 3)
	put(0, 1)
	put(0, 1)
	put(0, 1) // no transforms, cache, or meta-Huffman image
	for _, symbol := range []byte{100, 200, 50, alpha, 0} {
		put(1, 1)
		put(0, 1)
		put(1, 1)
		put(uint32(symbol), 8)
	}
	return riff(riffChunk("VP8L", stream))
}

func TestGenerateFormatsAndDimensions(t *testing.T) {
	// Minimal 1x1 VP8 key frame, not a user's cover image.
	lossy, err := base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
	if err != nil {
		t.Fatal(err)
	}
	if generate(t, lossy).Bounds() != image.Rect(0, 0, 1, 1) {
		t.Fatal("lossy WebP dimensions")
	}
	for _, size := range []image.Point{{12, 18}, {600, 600}, {1200, 800}, {800, 1200}, {2000, 1}, {1, 2000}} {
		t.Run(size.String(), func(t *testing.T) {
			img := image.NewNRGBA(image.Rect(0, 0, size.X, size.Y))
			img.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 128})
			data := encodePNG(t, img)
			before := bytes.Clone(data)
			out := generate(t, data)
			w, h := size.X, size.Y
			if w > 600 || h > 600 {
				if w >= h {
					h = max(1, h*600/w)
					w = 600
				} else {
					w = max(1, w*600/h)
					h = 600
				}
			}
			if out.Bounds() != image.Rect(0, 0, w, h) {
				t.Fatalf("bounds %v", out.Bounds())
			}
			if !bytes.Equal(before, data) {
				t.Fatal("input mutated")
			}
			if size.X == 12 {
				if color.NRGBAModel.Convert(out.At(0, 0)).(color.NRGBA).A != 128 {
					t.Fatal("alpha lost")
				}
			}
		})
	}
	var jpegData bytes.Buffer
	if err := jpeg.Encode(&jpegData, image.NewRGBA(image.Rect(0, 0, 3, 2)), nil); err != nil {
		t.Fatal(err)
	}
	if generate(t, jpegData.Bytes()).Bounds() != image.Rect(0, 0, 3, 2) {
		t.Fatal("JPEG dimensions")
	}
	for _, alpha := range []byte{255, 128, 0} {
		img := generate(t, losslessWebP(3, 2, alpha))
		if img.Bounds() != image.Rect(0, 0, 3, 2) {
			t.Fatal("WebP dimensions")
		}
		if color.NRGBAModel.Convert(img.At(0, 0)).(color.NRGBA).A != alpha {
			t.Fatal("WebP alpha")
		}
	}
}

func TestOrientationAllEight(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 2; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: byte(1 + x + 2*y), A: 255})
		}
	}
	want := [][]byte{{1, 2, 3, 4, 5, 6}, {2, 1, 4, 3, 6, 5}, {6, 5, 4, 3, 2, 1}, {5, 6, 3, 4, 1, 2}, {1, 3, 5, 2, 4, 6}, {5, 3, 1, 6, 4, 2}, {6, 4, 2, 5, 3, 1}, {2, 4, 6, 1, 3, 5}}
	for orientation := 1; orientation <= 8; orientation++ {
		for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
			out := generate(t, addPNGChunk(encodePNG(t, img), "eXIf", tiffOrientation(uint16(orientation), order)))
			var got []byte
			for y := 0; y < out.Bounds().Dy(); y++ {
				for x := 0; x < out.Bounds().Dx(); x++ {
					got = append(got, color.NRGBAModel.Convert(out.At(x, y)).(color.NRGBA).R)
				}
			}
			if !bytes.Equal(got, want[orientation-1]) {
				t.Fatalf("orientation %d: %v", orientation, got)
			}
		}
	}
}

func TestEXIFInJPEGAndWebP(t *testing.T) {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 2, 3)), nil); err != nil {
		t.Fatal(err)
	}
	payload := append([]byte("Exif\x00\x00"), tiffOrientation(6, binary.LittleEndian)...)
	segment := []byte{0xff, 0xe1, 0, byte(len(payload) + 2)}
	segment = append(segment, payload...)
	jpegData := append(bytes.Clone(b.Bytes()[:2]), segment...)
	jpegData = append(jpegData, b.Bytes()[2:]...)
	if generate(t, jpegData).Bounds() != image.Rect(0, 0, 3, 2) {
		t.Fatal("JPEG orientation")
	}
	webpData := losslessWebP(2, 3, 255)
	// Add a conforming VP8X header, canvas 2x3 and EXIF feature flag.
	extended := []byte{8, 0, 0, 0, 1, 0, 0, 2, 0, 0}
	webpData = riff(riffChunk("VP8X", extended), webpData[12:], riffChunk("EXIF", payload))
	if generate(t, webpData).Bounds() != image.Rect(0, 0, 3, 2) {
		t.Fatal("WebP orientation")
	}
}

func TestMalformedEXIF(t *testing.T) {
	valid := tiffOrientation(1, binary.LittleEndian)
	for _, tc := range []struct {
		name   string
		mutate func([]byte)
	}{
		{"byte order", func(b []byte) { b[0] = 'X' }},
		{"magic", func(b []byte) { b[2] = 0 }},
		{"offset overflow", func(b []byte) { binary.LittleEndian.PutUint32(b[4:], math.MaxUint32) }},
		{"offset into header", func(b []byte) { binary.LittleEndian.PutUint32(b[4:], 2) }},
		{"count overflow", func(b []byte) { binary.LittleEndian.PutUint16(b[8:], math.MaxUint16) }},
		{"wrong type", func(b []byte) { binary.LittleEndian.PutUint16(b[12:], 4) }},
		{"wrong count", func(b []byte) { binary.LittleEndian.PutUint32(b[14:], 2) }},
		{"zero value", func(b []byte) { binary.LittleEndian.PutUint16(b[18:], 0) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := bytes.Clone(valid)
			tc.mutate(b)
			if _, err := exifOrientation(b); !errors.Is(err, ErrOrientation) {
				t.Fatal(err)
			}
		})
	}
	// Valid metadata without Orientation leaves the image in normal direction.
	b := bytes.Clone(valid)
	binary.LittleEndian.PutUint16(b[10:], 0x0100)
	if orientation, err := exifOrientation(b); err != nil || orientation != 1 {
		t.Fatalf("%d %v", orientation, err)
	}
	// Duplicate orientation entries are not silently resolved by picking one.
	b = append(bytes.Clone(valid[:22]), valid[10:22]...)
	b = append(b, 0, 0, 0, 0)
	binary.LittleEndian.PutUint16(b[8:], 2)
	if _, err := exifOrientation(b); !errors.Is(err, ErrOrientation) {
		t.Fatal(err)
	}
}

func TestResizeKeepsWholeImageAndAlpha(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 1200, 800))
	for y := 0; y < 800; y++ {
		for x := 0; x < 1200; x++ {
			c := color.NRGBA{R: 200, G: 100, B: 50, A: 128}
			if x < 600 {
				c.R = 20
			}
			img.SetNRGBA(x, y, c)
		}
	}
	out := generate(t, encodePNG(t, img))
	for _, p := range []image.Point{{0, 0}, {599, 0}, {0, 399}, {599, 399}} {
		got := color.NRGBAModel.Convert(out.At(p.X, p.Y)).(color.NRGBA)
		wantR := 200
		if p.X == 0 {
			wantR = 20
		}
		if math.Abs(float64(int(got.R)-wantR)) > 2 || got.A != 128 {
			t.Fatalf("%v: %v", p, got)
		}
	}
	// View coordinates must work even when the source is not rooted at (0,0).
	source := image.NewNRGBA(image.Rect(4, 7, 6, 10))
	source.SetNRGBA(4, 9, color.NRGBA{R: 123, A: 255})
	view := orientedImage{source, 6}
	if color.NRGBAModel.Convert(view.At(0, 0)).(color.NRGBA).R != 123 {
		t.Fatal("non-zero origin")
	}
	if color.NRGBAModel.Convert(view.At(-1, 0)).(color.NRGBA).A != 0 {
		t.Fatal("out of bounds")
	}
}

func TestWebPContainerBoundaries(t *testing.T) {
	base := losslessWebP(2, 3, 255)
	for _, data := range [][]byte{
		riff(riffChunk("VP8X", []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0}), base[12:]), // canvas mismatch
		riff(riffChunk("VP8X", []byte{0})),
		riff(riffChunk("VP8X", []byte{1, 0, 0, 0, 1, 0, 0, 2, 0, 0}), base[12:]), // reserved flag
		riff(base[12:], riffChunk("VP8X", make([]byte, 10))),
		riff([]byte("junk")),
	} {
		if out, err := Generate(bytes.NewReader(data)); out != nil || !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
	// A non-zero RIFF pad byte is malformed, even in an unknown ancillary chunk.
	chunk := riffChunk("JUNK", []byte{1})
	chunk[len(chunk)-1] = 1
	if _, err := Generate(bytes.NewReader(riff(base[12:], chunk))); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

func TestInvalidInputs(t *testing.T) {
	valid := encodePNG(t, image.NewNRGBA(image.Rect(0, 0, 2, 3)))
	badCRC := bytes.Clone(valid)
	badCRC[29] ^= 1
	truncatedJPEG := []byte{0xff, 0xd8, 0xff, 0xe1, 0xff, 0xff, 0xff, 0xd9}
	badEXIF := tiffOrientation(9, binary.LittleEndian)
	oversizedChunk := bytes.Clone(valid)
	binary.BigEndian.PutUint32(oversizedChunk[8:12], math.MaxUint32)
	webp := losslessWebP(2, 3, 255)
	badRIFF := bytes.Clone(webp)
	binary.LittleEndian.PutUint32(badRIFF[4:], math.MaxUint32)
	tests := []struct {
		name string
		data []byte
		want error
	}{
		{"empty", nil, ErrUnsupported}, {"gif", []byte("GIF89a"), ErrUnsupported}, {"fake", []byte("not a PNG"), ErrUnsupported},
		{"png header only", valid[:33], ErrInvalid}, {"png CRC", badCRC, ErrInvalid}, {"png trailing", append(bytes.Clone(valid), 0), ErrInvalid},
		{"png chunk overflow", oversizedChunk, ErrInvalid}, {"jpeg segment", truncatedJPEG, ErrInvalid},
		{"bad orientation", addPNGChunk(valid, "eXIf", badEXIF), ErrOrientation}, {"bad tiff", addPNGChunk(valid, "eXIf", []byte{0}), ErrOrientation},
		{"duplicate exif", addPNGChunk(addPNGChunk(valid, "eXIf", tiffOrientation(1, binary.LittleEndian)), "eXIf", tiffOrientation(1, binary.LittleEndian)), ErrOrientation},
		{"riff size", badRIFF, ErrInvalid}, {"webp truncated", webp[:len(webp)-1], ErrInvalid},
		{"webp duplicate", riff(webp[12:], webp[12:]), ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Generate(bytes.NewReader(tc.data))
			if !errors.Is(err, tc.want) || out != nil {
				t.Fatalf("out %d err %v want %v", len(out), err, tc.want)
			}
		})
	}
}

func TestRejectAnimationMarkers(t *testing.T) {
	valid := encodePNG(t, image.NewNRGBA(image.Rect(0, 0, 2, 3)))
	for _, kind := range []string{"acTL", "fcTL", "fdAT"} {
		if _, err := Generate(bytes.NewReader(addPNGChunk(valid, kind, make([]byte, 8)))); !errors.Is(err, ErrAnimation) {
			t.Fatalf("%s: %v", kind, err)
		}
	}
	for _, kind := range []string{"ANIM", "ANMF"} {
		if _, err := Generate(bytes.NewReader(riff(riffChunk(kind, make([]byte, 8))))); !errors.Is(err, ErrAnimation) {
			t.Fatalf("%s: %v", kind, err)
		}
	}
	if _, err := Generate(bytes.NewReader(riff(riffChunk("VP8X", []byte{2, 0, 0, 0, 0, 0, 0, 0, 0, 0})))); !errors.Is(err, ErrAnimation) {
		t.Fatal(err)
	}
	// Marker text inside valid ancillary payload must not be mistaken for a chunk.
	generate(t, addPNGChunk(valid, "tEXt", []byte("comment\x00acTL fcTL fdAT ANIM ANMF")))
}

type countingReader struct{ remaining, read int }

func (r *countingReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := min(len(p), r.remaining)
	clear(p[:n])
	r.remaining -= n
	r.read += n
	return n, nil
}
func TestInputAndPixelLimits(t *testing.T) {
	// Equality at both public limits is accepted, not just the helper's values.
	pngData := encodePNG(t, image.NewNRGBA(image.Rect(0, 0, 1, 1)))
	exact := addPNGChunk(pngData, "ruSt", make([]byte, MaxInputBytes-len(pngData)-12))
	if len(exact) != MaxInputBytes {
		t.Fatal("fixture length")
	}
	generate(t, exact)
	if _, err := Generate(bytes.NewReader(append(exact, 0))); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	generate(t, encodePNG(t, image.NewNRGBA(image.Rect(0, 0, 4000, 5000))))
	r := &countingReader{remaining: MaxInputBytes + 100}
	if out, err := Generate(r); !errors.Is(err, ErrTooLarge) || out != nil || r.read != MaxInputBytes+1 {
		t.Fatalf("read %d err %v", r.read, err)
	}
	for _, tc := range []struct {
		w, h  int
		valid bool
	}{{4000, 5000, true}, {4000, 5001, false}, {0, 1, false}, {1, 0, false}, {-1, 1, false}, {math.MaxInt, math.MaxInt, false}, {20_000_000, 1, true}, {20_000_001, 1, false}} {
		if (validateDimensions(tc.w, tc.h) == nil) != tc.valid {
			t.Fatalf("%v", tc)
		}
	}
	// A huge IHDR is rejected before allocating its pixels.
	data := encodePNG(t, image.NewNRGBA(image.Rect(0, 0, 2, 3)))
	binary.BigEndian.PutUint32(data[16:20], 4000)
	binary.BigEndian.PutUint32(data[20:24], 5001)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	if _, err := Generate(bytes.NewReader(data)); !errors.Is(err, ErrDimensions) {
		t.Fatal(err)
	}
	// Small VP8X must not hide a large VP8L header.
	data = riff(riffChunk("VP8X", []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0}), losslessWebP(16384, 16384, 255)[12:])
	if _, err := Generate(bytes.NewReader(data)); !errors.Is(err, ErrDimensions) {
		t.Fatal(err)
	}
}

type failedReader struct{}

func (failedReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func TestReadFailureAndConcurrentCalls(t *testing.T) {
	if out, err := Generate(failedReader{}); out != nil || !errors.Is(err, ErrInvalid) || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
	data := encodePNG(t, image.NewNRGBA(image.Rect(0, 0, 2, 3)))
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Generate(bytes.NewReader(data)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

func FuzzGenerate(f *testing.F) {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 2, 3)))
	f.Add(b.Bytes())
	f.Add(losslessWebP(1, 1, 255))
	f.Add([]byte{0xff, 0xd8, 0xff, 0xd9})
	f.Fuzz(func(t *testing.T, data []byte) {
		out, err := Generate(bytes.NewReader(data))
		if err != nil {
			if out != nil {
				t.Fatal("partial output")
			}
			return
		}
		img, err := png.Decode(bytes.NewReader(out))
		if err != nil || img.Bounds().Dx() > 600 || img.Bounds().Dy() > 600 {
			t.Fatalf("output: %v", err)
		}
	})
}
