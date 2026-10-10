// These synthetic probes investigate APIs, not the application's image feature.
package imageboundaries

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	wailsassets "github.com/wailsapp/wails/v2/pkg/assetserver"
	assetoptions "github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

func TestDecodeConfigDoesNotValidatePayload(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	// PNG signature and IHDR are enough for DecodeConfig, but not Decode.
	headerOnly := encoded.Bytes()[:33]
	config, format, err := image.DecodeConfig(bytes.NewReader(headerOnly))
	if err != nil || format != "png" || config.Width != 2 || config.Height != 3 {
		t.Fatalf("header: %v, %s, %v", config, format, err)
	}
	if _, _, err := image.Decode(bytes.NewReader(headerOnly)); err == nil {
		t.Fatal("truncated payload was accepted")
	}
}

func TestStandardDecodersReadSyntheticImages(t *testing.T) {
	for _, format := range []string{"jpeg", "png"} {
		t.Run(format, func(t *testing.T) {
			var encoded bytes.Buffer
			img := image.NewNRGBA(image.Rect(0, 0, 12, 18))
			var err error
			if format == "jpeg" {
				err = jpeg.Encode(&encoded, img, nil)
			} else {
				err = png.Encode(&encoded, img)
			}
			if err != nil {
				t.Fatal(err)
			}
			config, gotFormat, err := image.DecodeConfig(bytes.NewReader(encoded.Bytes()))
			if err != nil || gotFormat != format || config.Width != 12 || config.Height != 18 {
				t.Fatalf("config: %v, %s, %v", config, gotFormat, err)
			}
			decoded, _, err := image.Decode(bytes.NewReader(encoded.Bytes()))
			if err != nil || decoded.Bounds().Dx() != 12 || decoded.Bounds().Dy() != 18 {
				t.Fatalf("decode: %v", err)
			}
		})
	}
}

func TestPNGDecoderDoesNotRejectAnimationControlChunk(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	// This is a synthetic animation marker, not a full APNG conformance fixture.
	chunk := make([]byte, 20)
	binary.BigEndian.PutUint32(chunk[:4], 8)
	copy(chunk[4:8], "acTL")
	binary.BigEndian.PutUint32(chunk[8:12], 2)
	binary.BigEndian.PutUint32(chunk[16:], crc32.ChecksumIEEE(chunk[4:16]))
	marked := append([]byte{}, encoded.Bytes()[:33]...)
	marked = append(marked, chunk...)
	marked = append(marked, encoded.Bytes()[33:]...)
	if _, _, err := image.Decode(bytes.NewReader(marked)); err != nil {
		t.Fatalf("decoder rejected synthetic ancillary chunk: %v", err)
	}
}

func TestWailsAssetHandlerAndMiddleware(t *testing.T) {
	static := fstest.MapFS{"index.html": {Data: []byte("static")}}
	handler, err := wailsassets.NewAssetHandler(assetoptions.Options{
		Assets: static,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Probe", "fallback")
			w.WriteHeader(http.StatusNotFound)
		}),
		Middleware: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/probe-cover.png" {
					w.Header().Set("Content-Type", "image/png")
					w.WriteHeader(http.StatusNoContent)
					return
				}
				next.ServeHTTP(w, r)
			})
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path string
		code int
	}{{"/probe-cover.png", http.StatusNoContent}, {"/missing.png", http.StatusNotFound}, {"/index.html", http.StatusOK}} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if recorder.Code != tc.code {
			t.Errorf("%s: %d, want %d", tc.path, recorder.Code, tc.code)
		}
		if tc.path == "/missing.png" && recorder.Header().Get("X-Probe") != "fallback" {
			t.Error("missing asset did not reach fallback")
		}
	}
}
