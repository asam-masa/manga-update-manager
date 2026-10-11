package main

import (
	"bytes"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	wailsassets "github.com/wailsapp/wails/v2/pkg/assetserver"
	assetoptions "github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

func newProbe(t *testing.T) (*Probe, string) {
	t.Helper()
	dir := t.TempDir()
	if err := fixtures(dir); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return &Probe{root: root, selected: "red.png"}, dir
}

func TestDeliveryThroughBothWailsHandlers(t *testing.T) {
	for _, external := range []bool{false, true} {
		p, _ := newProbe(t)
		settings := assetoptions.Options{Assets: assets, Middleware: p.middleware}
		var handler http.Handler
		if external {
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(418) }))
			defer proxy.Close()
			u, _ := url.Parse(proxy.URL)
			handler = wailsassets.NewExternalAssetsHandler(nil, settings, u)
		} else {
			var err error
			handler, err = wailsassets.NewAssetHandler(settings, nil)
			if err != nil {
				t.Fatal(err)
			}
		}
		for _, state := range []string{"red", "blue", "missing", "unset", "broken"} {
			if err := p.Select(state); err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest("GET", "/__cover_spike/image/1", nil))
			want := 200
			if state == "missing" || state == "unset" {
				want = 404
			}
			if w.Code != want || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("external=%v state=%s status=%d", external, state, w.Code)
			}
			if state == "red" || state == "blue" {
				img, err := png.Decode(bytes.NewReader(w.Body.Bytes()))
				if err != nil {
					t.Fatal(err)
				}
				r, _, b, _ := img.At(0, 0).RGBA()
				if (state == "red" && (r != 65535 || b != 0)) || (state == "blue" && (r != 0 || b != 65535)) {
					t.Fatal("stale image")
				}
			}
		}
	}
}

func TestRejectPathsAndMethods(t *testing.T) {
	p, _ := newProbe(t)
	h := p.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(418) }))
	for _, target := range []string{"/__cover_spike/image/../red.png", "/__cover_spike/image/%31", "/__cover_spike/image/%2531", "/__cover_spike/image/%2e%2e%2fsecret", "/__cover_spike/image/C:%5csecret", "/__cover_spike/image/1?path=red.png", "/__cover_spike/image/999"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", target, nil))
		if w.Code != 404 {
			t.Errorf("%s: %d", target, w.Code)
		}
	}
	for _, method := range []string{"POST", "PUT", "DELETE", "HEAD"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, "/__cover_spike/image/1", nil))
		if w.Code != 405 {
			t.Fatal(method, w.Code)
		}
	}
	if p.Select("../../secret") == nil {
		t.Fatal("accepted arbitrary path")
	}
}

func TestRejectOutsideSymlink(t *testing.T) {
	p, dir := newProbe(t)
	outside := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(outside, []byte("outside secret"), 0600); err != nil {
		t.Fatal(err)
	}
	// Use a missing fixed fixture name; never replace real or user-owned files.
	if err := os.Symlink(outside, filepath.Join(dir, "missing.png")); err != nil {
		t.Skip("symlink creation unavailable", err)
	}
	if err := p.Select("missing"); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	p.middleware(http.NotFoundHandler()).ServeHTTP(w, httptest.NewRequest("GET", "/__cover_spike/image/1", nil))
	if w.Code != 404 || bytes.Contains(w.Body.Bytes(), []byte("outside secret")) {
		t.Fatal("outside link served")
	}
}
