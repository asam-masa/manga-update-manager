package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
	"github.com/asam-masa/manga-update-manager/internal/thumbnail"
)

type coverHTTPReader interface {
	Read(context.Context, int64) ([]byte, error)
}

// This lock protects DB lifetime only during the buffered read, not transmission.
func (a *App) readHTTPCover(ctx context.Context, id int64) ([]byte, error) {
	select {
	case <-a.startupFinished:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.coverReader == nil || a.stopped {
		return nil, application.ErrCoverStopped
	}
	return a.coverReader.Read(ctx, id)
}

func (a *App) coverMiddleware(next http.Handler) http.Handler {
	// Retain a slot through Write, bounding both reading and buffered responses.
	// Waiting or sending never holds App/reference locks or an operation ticket.
	slots := make(chan struct{}, 4)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/__covers" && !strings.HasPrefix(r.URL.Path, "/__covers/") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		const prefix = "/__covers/works/"
		idText := strings.TrimPrefix(r.URL.Path, prefix)
		id, err := strconv.ParseInt(idText, 10, 64)
		if !strings.HasPrefix(r.URL.Path, prefix) || err != nil || id <= 0 || strconv.FormatInt(id, 10) != idText ||
			r.URL.RawPath != "" || r.URL.EscapedPath() != r.URL.Path || r.URL.RawQuery != "" || r.URL.ForceQuery || r.URL.Fragment != "" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		case <-r.Context().Done():
			http.Error(w, "cover unavailable", http.StatusServiceUnavailable)
			return
		}
		data, err := a.readHTTPCover(r.Context(), id)
		if err != nil {
			status := http.StatusServiceUnavailable
			for _, missing := range []error{application.ErrCoverUnavailable, manga.ErrWorkNotFound, storage.ErrManagedCoverNotFound,
				platform.ErrInvalidCoverFile, os.ErrNotExist, thumbnail.ErrInvalid, thumbnail.ErrAnimation, thumbnail.ErrOrientation} {
				if errors.Is(err, missing) {
					status = http.StatusNotFound
					break
				}
			}
			// Safety-stop markers take precedence over an accompanying absence.
			if errors.Is(err, storage.ErrDBStopped) || errors.Is(err, platform.ErrCoverRelease) || errors.Is(err, platform.ErrUnsafeCoverDirectory) {
				status = http.StatusServiceUnavailable
			}
			http.Error(w, "cover unavailable", status)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		// A transport failure cannot undo the read or mutate its DB reference.
		_, _ = w.Write(data)
	})
}
