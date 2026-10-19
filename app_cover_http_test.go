package main

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
	wailsassets "github.com/wailsapp/wails/v2/pkg/assetserver"
	assetoptions "github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

type coverHTTPReaderFunc func(context.Context, int64) ([]byte, error)

func (f coverHTTPReaderFunc) Read(ctx context.Context, id int64) ([]byte, error) { return f(ctx, id) }

func coverHTTPFixture(t *testing.T) (*App, string) {
	t.Helper()
	dir := t.TempDir()
	a := NewApp()
	a.dataDirectory = func() (string, error) { return dir, nil }
	a.chooseCover = func(context.Context) (string, error) { return filepath.Join(dir, "source.png"), nil }
	a.startup(context.Background())
	if _, err := a.LoadCoverWarnings(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.shutdown(context.Background()) })
	return a, dir
}

func TestCoverHTTPThroughWailsHandlers(t *testing.T) {
	for _, external := range []bool{false, true} {
		a, dir := coverHTTPFixture(t)
		var source bytes.Buffer
		if err := png.Encode(&source, image.NewNRGBA(image.Rect(0, 0, 3, 4))); err != nil {
			t.Fatal(err)
		}
		name := filepath.Join(dir, "source.png")
		if err := os.WriteFile(name, source.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		work, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/synthetic", Title: "Synthetic"})
		if err != nil {
			t.Fatal(err)
		}
		settings := assetoptions.Options{Assets: assets, Middleware: a.coverMiddleware}
		var h http.Handler
		if external {
			proxy := httptest.NewServer(http.NotFoundHandler())
			defer proxy.Close()
			u, _ := url.Parse(proxy.URL)
			h = wailsassets.NewExternalAssetsHandler(nil, settings, u)
		} else {
			h, err = wailsassets.NewAssetHandler(settings, nil)
			if err != nil {
				t.Fatal(err)
			}
		}
		get := func(want int) []byte {
			t.Helper()
			for _, query := range []string{"?v=0123456789abcdef0123456789abcdef", "?v=abcdef0123456789abcdef0123456789"} {
				w := httptest.NewRecorder()
				h.ServeHTTP(w, httptest.NewRequest("GET", "/__covers/works/1"+query, nil))
				if w.Code != want || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
					t.Fatalf("external=%v query=%s: %d", external, query, w.Code)
				}
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "/__covers/works/1", nil))
			if w.Code != want || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatalf("external=%v: %d", external, w.Code)
			}
			if want == 200 {
				if w.Header().Get("Content-Type") != "image/png" {
					t.Fatal(w.Header())
				}
				if _, err := png.Decode(bytes.NewReader(w.Body.Bytes())); err != nil {
					t.Fatal(err)
				}
			}
			return w.Body.Bytes()
		}
		get(404)
		first, err := a.SelectWorkCover(work.ID)
		if err != nil {
			t.Fatal(err)
		}
		get(200)
		before, err := a.store.WorkByID(context.Background(), work.ID)
		if err != nil {
			t.Fatal(err)
		}
		get(200)
		after, err := a.store.WorkByID(context.Background(), work.ID)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("HTTP read changed work", err)
		}
		second, err := a.SelectWorkCover(work.ID)
		if err != nil || second.Work.ThumbnailPath == first.Work.ThumbnailPath {
			t.Fatal(err)
		}
		get(200)
		managed := filepath.Join(dir, filepath.FromSlash(second.Work.ThumbnailPath))
		if err := os.Rename(managed, managed+"-retained"); err != nil {
			t.Fatal(err)
		}
		get(404)
		if err := os.Rename(managed+"-retained", managed); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(managed, []byte("corrupt"), 0600); err != nil {
			t.Fatal(err)
		}
		get(404)
		stored, err := a.store.WorkByID(context.Background(), work.ID)
		if err != nil || stored.ThumbnailPath != second.Work.ThumbnailPath {
			t.Fatal("missing/corrupt reference cleared", err)
		}
		if _, err := a.RemoveWorkCover(work.ID); err != nil {
			t.Fatal(err)
		}
		get(404)
	}
}

func TestCoverHTTPRejectsBeforeReading(t *testing.T) {
	a := NewApp()
	close(a.startupFinished)
	a.coverReader = coverHTTPReaderFunc(func(context.Context, int64) ([]byte, error) { t.Fatal("invalid request read data"); return nil, nil })
	h := a.coverMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(418) }))
	for _, query := range []string{"v=", "v=" + strings.Repeat("a", 31), "v=" + strings.Repeat("a", 33),
		"v=" + strings.Repeat("A", 32), "v=" + strings.Repeat("g", 32), "%76=" + strings.Repeat("a", 32),
		"v=%61" + strings.Repeat("a", 31), "v=" + strings.Repeat("a", 32) + "&x=1",
		"v=" + strings.Repeat("a", 32) + "&v=" + strings.Repeat("b", 32)} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/__covers/works/1?"+query, nil))
		if w.Code != 404 {
			t.Fatalf("invalid query %s: %d", query, w.Code)
		}
	}
	for _, target := range []string{"/__covers", "/__covers/works/0", "/__covers/works/-1", "/__covers/works/+1", "/__covers/works/01", "/__covers/works/9223372036854775808", "/__covers/works/1/", "/__covers/works/../1", "/__covers/works/%31", "/__covers/works/%2531", "/__covers%2fworks%2f1", "/__covers/works/1?x=1", "/__covers/works/1?", "/__covers/works/C:%5csecret"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", target, nil))
		if w.Code != 404 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%s: %d", target, w.Code)
		}
	}
	for _, method := range []string{"POST", "PUT", "DELETE", "HEAD", "OPTIONS"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, "/__covers/works/1", nil))
		if w.Code != 405 || w.Header().Get("Allow") != "GET" {
			t.Fatal(method, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/normal", nil))
	if w.Code != 418 {
		t.Fatal("normal assets intercepted")
	}
}

func TestCoverHTTPTokenDoesNotSelectHistoricalData(t *testing.T) {
	a := NewApp()
	close(a.startupFinished)
	current := []byte("first image")
	a.coverReader = coverHTTPReaderFunc(func(_ context.Context, id int64) ([]byte, error) {
		if id != 7 {
			t.Fatalf("unexpected work ID %d", id)
		}
		return current, nil
	})
	h := a.coverMiddleware(http.NotFoundHandler())
	for _, value := range []string{"first image", "replacement image"} {
		current = []byte(value)
		for _, query := range []string{"", "?v=" + strings.Repeat("a", 32), "?v=" + strings.Repeat("b", 32)} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "/__covers/works/7"+query, nil))
			if w.Code != 200 || w.Body.String() != value || w.Header().Get("Content-Type") != "image/png" {
				t.Fatalf("query=%s: %d %q", query, w.Code, w.Body.String())
			}
		}
	}
}

func TestCoverHTTPDoesNotExposeErrors(t *testing.T) {
	for _, failure := range []error{errors.New(`D:\private\secret.png`), os.ErrNotExist, errors.Join(os.ErrNotExist, platform.ErrCoverRelease), storage.ErrDBStopped} {
		a := NewApp()
		close(a.startupFinished)
		a.coverReader = coverHTTPReaderFunc(func(context.Context, int64) ([]byte, error) { return []byte("private data"), failure })
		w := httptest.NewRecorder()
		a.coverMiddleware(http.NotFoundHandler()).ServeHTTP(w, httptest.NewRequest("GET", "/__covers/works/1", nil))
		want := 503
		if failure == os.ErrNotExist {
			want = 404
		}
		if w.Code != want || strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), failure.Error()) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

type blockedCoverWriter struct {
	*httptest.ResponseRecorder
	entered, release chan struct{}
}

func (w *blockedCoverWriter) Write(data []byte) (int, error) {
	close(w.entered)
	<-w.release
	return w.ResponseRecorder.Write(data)
}

func TestCoverHTTPTransmissionDoesNotHoldAppAndBoundsBuffers(t *testing.T) {
	a, _ := coverHTTPFixture(t)
	a.coverReader = coverHTTPReaderFunc(func(context.Context, int64) ([]byte, error) { return []byte("buffer"), nil })
	h := a.coverMiddleware(http.NotFoundHandler())
	var wg sync.WaitGroup
	writers := make([]*blockedCoverWriter, 4)
	for i := range writers {
		w := &blockedCoverWriter{httptest.NewRecorder(), make(chan struct{}), make(chan struct{})}
		writers[i] = w
		wg.Go(func() { h.ServeHTTP(w, httptest.NewRequest("GET", "/__covers/works/1", nil)) })
		<-w.entered
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/__covers/works/1", nil).WithContext(ctx))
	if w.Code != 503 {
		t.Fatal("fifth request did not wait/cancel", w.Code)
	}
	// A slow response must not keep storage or the App lock alive.
	a.shutdown(context.Background())
	for _, writer := range writers {
		close(writer.release)
	}
	wg.Wait()
}

func TestCoverHTTPStartupWaitCanCancel(t *testing.T) {
	a := NewApp()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if data, err := a.readHTTPCover(ctx, 1); data != nil || !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	a.shutdown(context.Background())
	if data, err := a.readHTTPCover(context.Background(), 1); data != nil || err == nil {
		t.Fatal("read after shutdown")
	}
}

func TestCoverHTTPReadProtectsDatabaseLifetime(t *testing.T) {
	a, _ := coverHTTPFixture(t)
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	a.coverReader = coverHTTPReaderFunc(func(context.Context, int64) ([]byte, error) {
		close(entered)
		<-release
		return []byte("snapshot"), nil
	})
	go func() { defer close(finished); _, _ = a.readHTTPCover(context.Background(), 1) }()
	<-entered
	if a.mu.TryLock() {
		a.mu.Unlock()
		t.Fatal("DB lifetime not protected during read")
	}
	closed := make(chan struct{})
	go func() { a.shutdown(context.Background()); close(closed) }()
	select {
	case <-closed:
		t.Fatal("shutdown before read release")
	default:
	}
	close(release)
	<-finished
	<-closed
}
