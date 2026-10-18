package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

func TestSelectWorkCoverAPI(t *testing.T) {
	dir := t.TempDir()
	var source bytes.Buffer
	if err := png.Encode(&source, image.NewNRGBA(image.Rect(0, 0, 3, 4))); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(dir, "source.png")
	if err := os.WriteFile(sourcePath, source.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	app.dataDirectory = func() (string, error) { return dir, nil }
	selected := sourcePath
	app.chooseCover = func(context.Context) (string, error) { return selected, nil }
	app.startup(context.Background())
	defer app.shutdown(context.Background())
	work, err := app.CreateWork(CreateWorkInput{URL: "https://example.com/synthetic", Title: "Synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	selected = ""
	cancelled, err := app.SelectWorkCover(work.ID)
	if err != nil || !cancelled.Cancelled || cancelled.Work != nil || len(cancelled.Warnings) != 0 {
		t.Fatal(cancelled, err)
	}
	encoded, _ := json.Marshal(cancelled)
	if !strings.Contains(string(encoded), `"warnings":[]`) || !strings.Contains(string(encoded), `"work":null`) {
		t.Fatal(string(encoded))
	}
	selected = sourcePath
	first, err := app.SelectWorkCover(work.ID)
	if err != nil || first.Work == nil || first.Cancelled || len(first.Warnings) != 0 {
		t.Fatal(first, err)
	}
	second, err := app.SelectWorkCover(work.ID)
	if err != nil || second.Work == nil || first.Work.ThumbnailPath == second.Work.ThumbnailPath || len(second.Warnings) != 1 || second.Warnings[0].Code != "cover_cleanup_deferred" {
		t.Fatal(second, err)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(first.Work.ThumbnailPath))); err != nil {
		t.Fatal("old managed image was removed", err)
	}
	after, err := os.ReadFile(sourcePath)
	if err != nil || !bytes.Equal(after, source.Bytes()) {
		t.Fatal("source changed", err)
	}
	encoded, _ = json.Marshal(second)
	if strings.Contains(string(encoded), sourcePath) {
		t.Fatal("source path exposed")
	}
	if err := app.store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ListWorks(); errorCode(t, err) != "database_stopped" {
		t.Fatal(err)
	}
	if _, err := app.SelectWorkCover(work.ID); errorCode(t, err) != "cover_unavailable" {
		t.Fatal("DB error not observed by image gate", err)
	}
}

func TestBeforeCloseWithImageOperation(t *testing.T) {
	app := NewApp()
	notices := 0
	app.coverBusyNotice = func(context.Context) { notices++ }
	op, err := app.coverOperations.BeginChange(1, true)
	if err != nil {
		t.Fatal(err)
	}
	if !app.beforeClose(context.Background()) || notices != 1 {
		t.Fatal("close not refused")
	}
	if err := op.End(); err != nil {
		t.Fatal(err)
	}
	if app.beforeClose(context.Background()) || notices != 1 {
		t.Fatal("close still refused")
	}
	if _, err := app.coverOperations.BeginChange(1, true); !errors.Is(err, application.ErrCoverClosing) {
		t.Fatal(err)
	}
}

func TestCoverAPIErrorSafety(t *testing.T) {
	for _, tc := range []struct {
		failure error
		code    string
	}{
		{errors.Join(storage.ErrWorkCoverOutcomeUnknown, storage.ErrDBStopped), "cover_outcome_unknown"},
		{platform.ErrCoverRelease, "cover_unavailable"},
		{application.ErrCoverBusy, "cover_busy"},
		{errors.New("private source path"), "cover_import_failed"},
	} {
		got := coverAPIError(errors.Join(tc.failure, errors.New("private source path")))
		if errorCode(t, got) != tc.code || strings.Contains(got.Error(), "private") {
			t.Fatal(got)
		}
	}
}
