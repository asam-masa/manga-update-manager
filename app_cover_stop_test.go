package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

type coverStopRepository struct {
	application.CoverRepository
	afterConfirm func(context.Context) error
}

func (r coverStopRepository) ConfirmManagedCover(ctx context.Context, id string, info storage.ManagedCoverInfo) error {
	if err := r.CoverRepository.ConfirmManagedCover(ctx, id, info); err != nil {
		return err
	}
	return r.afterConfirm(ctx)
}

func coverStopFixture(t *testing.T) (*App, string, []byte, WorkDTO) {
	t.Helper()
	app, dir := coverHTTPFixture(t)
	var source bytes.Buffer
	if err := png.Encode(&source, image.NewNRGBA(image.Rect(0, 0, 3, 4))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "source.png"), source.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	work, err := app.CreateWork(CreateWorkInput{URL: "https://example.com/stop-probe", Title: "Synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := app.SelectWorkCover(work.ID)
	if err != nil || result.Work == nil {
		t.Fatal(result, err)
	}
	return app, dir, source.Bytes(), *result.Work
}

func TestCoverAPIRealCommitBusyPreservesBothImages(t *testing.T) {
	app, dir, source, before := coverStopFixture(t)
	oldPath := filepath.Join(dir, filepath.FromSlash(before.ThumbnailPath))
	old, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	observer, err := sql.Open("sqlite", filepath.Join(dir, "manga.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	var read *sql.Tx
	defer func() {
		if read != nil {
			_ = read.Rollback()
		}
	}()
	repository := coverStopRepository{CoverRepository: app.store, afterConfirm: func(ctx context.Context) error {
		var err error
		read, err = observer.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		var title string
		return read.QueryRowContext(ctx, "SELECT title FROM works WHERE id=?", before.ID).Scan(&title)
	}}
	app.ctx = ctx
	app.coverSelectionFinished = func(context.Context, int64, string) {} // No Wails event loop in this API test.
	app.covers = application.NewCoverImporter(repository, &app.coverOperations, dir, app.chooseCover, time.Now)
	result, err := app.SelectWorkCoverWithOperation(before.ID, strings.Repeat("a", 32))
	if errorCode(t, err) != "cover_outcome_unknown" || result.Work != nil || result.Cancelled {
		t.Fatal(result, err)
	}
	if safe := decodeFormattedError(t, err); safe.Code != "cover_outcome_unknown" || safe.Message != err.Error() {
		t.Fatal("outcome lost during error formatting", safe)
	}
	if !strings.Contains(err.Error(), "再起動") || strings.Contains(err.Error(), dir) {
		t.Fatal("unsafe outcome message", err)
	}
	if err := read.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ListWorks(); errorCode(t, err) != "database_stopped" {
		t.Fatal(err)
	}
	if _, err := app.CreateWork(CreateWorkInput{URL: "https://example.com/rejected", Title: "Rejected"}); errorCode(t, err) != "database_stopped" {
		t.Fatal(err)
	}
	if _, err := app.SelectWorkCover(before.ID); errorCode(t, err) != "cover_unavailable" {
		t.Fatal(err)
	}
	if _, err := app.RemoveWorkCover(before.ID); errorCode(t, err) != "cover_unavailable" {
		t.Fatal(err)
	}
	files, err := os.ReadDir(filepath.Join(dir, "covers"))
	if err != nil || len(files) != 2 {
		t.Fatal("new/old output not retained", files, err)
	}
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(dir, "covers", file.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := png.Decode(bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
	}
	if after, err := os.ReadFile(oldPath); err != nil || !bytes.Equal(after, old) {
		t.Fatal("old image changed", err)
	}
	if after, err := os.ReadFile(filepath.Join(dir, "source.png")); err != nil || !bytes.Equal(after, source) {
		t.Fatal("source changed", err)
	}
	// Reopen without startup cleanup: inspect the real stored outcome, not an API retry.
	app.shutdown(ctx)
	reopened, err := storage.Open(ctx, filepath.Join(dir, "manga.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	stored, err := reopened.WorkByID(ctx, before.ID)
	if err != nil || !reflect.DeepEqual(workDTO(stored), before) {
		t.Fatal("stored work changed", stored, err)
	}
	records, err := reopened.ListManagedCovers(ctx)
	if err != nil || len(records) != 2 {
		t.Fatal("management records lost", records, err)
	}
}

func TestCoverAPIImageOnlyStopKeepsDatabaseUsable(t *testing.T) {
	app, dir, source, before := coverStopFixture(t)
	// A synthetic release marker at the chooser boundary, not a real Close fault.
	choose := func(context.Context) (string, error) {
		return "", errors.Join(platform.ErrCoverRelease, errors.New("private source path"))
	}
	app.covers = application.NewCoverImporter(app.store, &app.coverOperations, dir, choose, time.Now)
	result, err := app.SelectWorkCover(before.ID)
	if errorCode(t, err) != "cover_unavailable" || result.Work != nil {
		t.Fatal(result, err)
	}
	if safe := decodeFormattedError(t, err); safe.Code != "cover_unavailable" || safe.Message != err.Error() {
		t.Fatal("image stop lost during error formatting", safe)
	}
	if strings.Contains(err.Error(), "private") {
		t.Fatal("internal error exposed", err)
	}
	if _, err := app.RemoveWorkCover(before.ID); errorCode(t, err) != "cover_unavailable" {
		t.Fatal(err)
	}
	works, err := app.ListWorks()
	if err != nil || len(works) != 1 || !reflect.DeepEqual(works[0], before) {
		t.Fatal(works, err)
	}
	if _, err := app.CreateWork(CreateWorkInput{URL: "https://example.com/still-usable", Title: "Still usable"}); err != nil {
		t.Fatal(err)
	}
	if after, err := os.ReadFile(filepath.Join(dir, "source.png")); err != nil || !bytes.Equal(after, source) {
		t.Fatal("source changed", err)
	}
	files, err := os.ReadDir(filepath.Join(dir, "covers"))
	if err != nil || len(files) != 1 {
		t.Fatal("image files changed", files, err)
	}
	encoded, err := json.Marshal(result)
	if err != nil || strings.Contains(string(encoded), dir) {
		t.Fatal(string(encoded), err)
	}
}
