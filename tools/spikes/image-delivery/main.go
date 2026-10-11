// A synthetic Wails probe, never connected to the application's DB or API.
package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed frontend/index.html frontend/probe.js
var assets embed.FS

type Probe struct {
	mu       sync.Mutex
	root     *os.Root
	selected string
}

// Select changes a synthetic reference, not a file or an application record.
func (p *Probe) Select(state string) error {
	names := map[string]string{"red": "red.png", "blue": "blue.png", "missing": "missing.png", "broken": "broken.png", "unset": ""}
	name, ok := names[state]
	if !ok {
		return fmt.Errorf("unknown synthetic state")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.selected = name
	return nil
}

type Result struct {
	Initial         bool `json:"initial"`
	Replacement     bool `json:"replacement"`
	Missing         bool `json:"missing"`
	Broken          bool `json:"broken"`
	Unset           bool `json:"unset"`
	MethodRejected  bool `json:"methodRejected"`
	EncodedRejected bool `json:"encodedRejected"`
	UnknownRejected bool `json:"unknownRejected"`
	NoStore         bool `json:"noStore"`
}

// Report stores only typed synthetic observations, without arbitrary paths.
func (p *Probe) Report(result Result) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	if err = p.root.WriteFile("result.json", data, 0600); err != nil {
		return fmt.Errorf("cannot write probe result")
	}
	fmt.Println("Synthetic WebView observations:", string(data))
	return nil
}

func (p *Probe) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/__cover_spike/") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.URL.Path != "/__cover_spike/image/1" || r.URL.EscapedPath() != r.URL.Path || r.URL.RawQuery != "" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		p.mu.Lock()
		name := p.selected
		p.mu.Unlock()
		if name == "" {
			http.NotFound(w, r)
			return
		}
		info, err := p.root.Lstat(name)
		if err != nil || !info.Mode().IsRegular() || isReparse(info) {
			http.NotFound(w, r)
			return
		}
		file, err := p.root.Open(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, 64*1024+1))
		if err != nil || len(data) > 64*1024 {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(data)
	})
}

func fixtures(dir string) error {
	for name, tint := range map[string]color.NRGBA{"red.png": {R: 255, A: 255}, "blue.png": {B: 255, A: 255}} {
		img := image.NewNRGBA(image.Rect(0, 0, 16, 24))
		for y := 0; y < 24; y++ {
			for x := 0; x < 16; x++ {
				img.SetNRGBA(x, y, tint)
			}
		}
		var data bytes.Buffer
		if err := png.Encode(&data, img); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name), data.Bytes(), 0600); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(dir, "broken.png"), []byte("synthetic invalid PNG"), 0600)
}

func main() {
	// No UserConfigDir or production storage. Retain artifacts for inspection.
	dir, err := os.MkdirTemp("", "manga-image-delivery-")
	if err != nil {
		panic("cannot create synthetic probe directory")
	}
	if err = fixtures(dir); err != nil {
		panic("cannot create synthetic fixtures")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		panic("cannot open synthetic probe directory")
	}
	defer root.Close()
	fmt.Println("Synthetic probe artifacts:", dir)
	probe := &Probe{root: root, selected: "red.png"}
	err = wails.Run(&options.App{
		Title: "画像表示の事前検証（仮画像専用）", Width: 780, Height: 680,
		AssetServer: &assetserver.Options{Assets: assets, Middleware: probe.middleware},
		Bind:        []interface{}{probe},
	})
	if err != nil {
		fmt.Println("probe startup failed")
		os.Exit(1)
	}
}
