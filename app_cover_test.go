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
	"sync"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/manga"
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
	if err != nil || second.Work == nil || first.Work.ThumbnailPath == second.Work.ThumbnailPath || len(second.Warnings) != 0 {
		t.Fatal(second, err)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(first.Work.ThumbnailPath))); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("old unreferenced managed image remains", err)
	}
	after, err := os.ReadFile(sourcePath)
	if err != nil || !bytes.Equal(after, source.Bytes()) {
		t.Fatal("source changed", err)
	}
	encoded, _ = json.Marshal(second)
	if strings.Contains(string(encoded), sourcePath) {
		t.Fatal("source path exposed")
	}
	removed, err := app.RemoveWorkCover(work.ID)
	if err != nil || removed.Work.ThumbnailPath != "" || len(removed.Warnings) != 0 {
		t.Fatal(removed, err)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(second.Work.ThumbnailPath))); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("removed image remains", err)
	}
	encoded, _ = json.Marshal(removed)
	if !strings.Contains(string(encoded), `"warnings":[]`) || strings.Contains(string(encoded), `"cancelled"`) || strings.Contains(string(encoded), sourcePath) {
		t.Fatal(string(encoded))
	}
	again, err := app.RemoveWorkCover(work.ID)
	if err != nil || again.Work.UpdatedAt != removed.Work.UpdatedAt || len(again.Warnings) != 0 {
		t.Fatal("no-op removal changed result", again, err)
	}
	after, err = os.ReadFile(sourcePath)
	if err != nil || !bytes.Equal(after, source.Bytes()) {
		t.Fatal("removal changed source", err)
	}
	if _, err := app.RemoveWorkCover(0); errorCode(t, err) != "invalid_input" {
		t.Fatal(err)
	}
	if _, err := app.RemoveWorkCover(work.ID + 1); errorCode(t, err) != "work_not_found" {
		t.Fatal(err)
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
	if _, err := app.RemoveWorkCover(work.ID); errorCode(t, err) != "cover_unavailable" {
		t.Fatal(err)
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

func TestRemoveWorkCoverWarningsAndErrorSafety(t *testing.T) {
	dir := t.TempDir()
	app := NewApp()
	app.dataDirectory = func() (string, error) { return dir, nil }
	app.startup(context.Background())
	defer app.shutdown(context.Background())
	work, err := app.CreateWork(CreateWorkInput{URL: "https://example.com/legacy", Title: "Legacy", ThumbnailPath: "legacy/original.png"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := app.RemoveWorkCover(work.ID)
	if err != nil || result.Work.ThumbnailPath != "" || len(result.Warnings) != 1 || result.Warnings[0].Code != "cover_cleanup_failed" || !strings.HasPrefix(result.Warnings[0].Message, "画像を解除しましたが、") {
		t.Fatal(result, err)
	}
	encoded, err := json.Marshal(result)
	if err != nil || strings.Contains(string(encoded), dir) || strings.Contains(string(encoded), "legacy/original.png") {
		t.Fatal(string(encoded), err)
	}
	// A zero injected clock reaches an ordinary Store rejection, not a commit.
	app.covers = application.NewCoverImporter(app.store, &app.coverOperations, dir, nil, func() time.Time { return time.Time{} })
	if _, err := app.RemoveWorkCover(work.ID); errorCode(t, err) != "cover_remove_failed" || strings.Contains(err.Error(), dir) {
		t.Fatal(err)
	}
	if _, err := NewApp().RemoveWorkCover(work.ID); errorCode(t, err) != "database_unavailable" {
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

type referenceWaitContext struct {
	context.Context
	checked chan struct{}
	once    sync.Once
}

func (c *referenceWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.checked) })
	return c.Context.Done()
}

type referenceCreateRepository struct {
	application.WorkRepository
	entered chan struct{}
}

func (r referenceCreateRepository) CreateWork(ctx context.Context, work manga.Work) (manga.Work, error) {
	r.entered <- struct{}{}
	return r.WorkRepository.CreateWork(ctx, work)
}

func TestCreateWorkJoinsImageReferenceGate(t *testing.T) {
	app := NewApp()
	app.dataDirectory = func() (string, error) { return t.TempDir(), nil }
	app.startup(context.Background())
	defer app.shutdown(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wait := &referenceWaitContext{Context: ctx, checked: make(chan struct{})}
	app.ctx = wait
	entered := make(chan struct{}, 1)
	app.works = application.NewWorks(referenceCreateRepository{app.store, entered}, time.Now)
	unlock, err := app.coverOperations.LockReferences(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	done := make(chan error, 1)
	go func() {
		_, err := app.CreateWork(CreateWorkInput{URL: "https://example.com/ref", Title: "Synthetic", ThumbnailPath: "covers/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.png"})
		done <- err
	}()
	select {
	case <-wait.checked:
	case <-time.After(10 * time.Second):
		t.Fatal("registration did not reach reference wait")
	}
	select {
	case <-entered:
		t.Fatal("registration repository started inside another reference section")
	default:
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled registration succeeded")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("reference wait ignored cancellation")
	}
	unlock()
	app.ctx = context.Background()
	app.coverOperations.Stop()
	if _, err := app.CreateWork(CreateWorkInput{URL: "https://example.com/ref", Title: "Synthetic"}); err != nil {
		t.Fatal("image-only stop prevented registration", err)
	}
}
