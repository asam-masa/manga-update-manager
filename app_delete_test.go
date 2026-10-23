package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

func deleteSnapshot(w WorkDTO) DeleteWorkInput {
	return DeleteWorkInput{URL: w.URL, Title: w.Title, SiteName: w.SiteName, Notes: w.Notes, ThumbnailPath: w.ThumbnailPath}
}

func TestAppDeleteWorkLifecycle(t *testing.T) {
	if _, err := NewApp().DeleteWork(1, DeleteWorkInput{}); errorCode(t, err) != "database_unavailable" {
		t.Fatal(err)
	}
	dir := t.TempDir()
	a := testApp(t, dir)
	w, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/delete", Title: "Delete", SiteName: "Site", Notes: "Note"})
	if err != nil {
		t.Fatal(err)
	}
	expected := deleteSnapshot(w)
	expected.Title += " "
	_, err = a.DeleteWork(w.ID, expected)
	editCode(t, err, "work_changed")
	got, err := a.DeleteWork(w.ID, deleteSnapshot(w))
	if err != nil || got.WorkID != w.ID || got.Warnings == nil || len(got.Warnings) != 0 {
		t.Fatal(got, err)
	}
	_, err = a.DeleteWork(w.ID, deleteSnapshot(w))
	editCode(t, err, "work_not_found")
	// Image-only stop must not make ordinary deletion unavailable.
	w, err = a.CreateWork(CreateWorkInput{URL: "https://example.com/stopped-image", Title: "Legacy", ThumbnailPath: "legacy/synthetic.png"})
	if err != nil {
		t.Fatal(err)
	}
	a.coverOperations.Stop()
	got, err = a.DeleteWork(w.ID, deleteSnapshot(w))
	if err != nil || got.WorkID != w.ID || len(got.Warnings) != 2 {
		t.Fatal(got, err)
	}
	for _, warning := range got.Warnings {
		if !strings.Contains(warning.Message, "作品は削除しましたが") {
			t.Fatal(warning)
		}
	}
	a.shutdown(context.Background())
	_, err = a.DeleteWork(w.ID, deleteSnapshot(w))
	editCode(t, err, "database_unavailable")
	s, err := storage.Open(context.Background(), filepath.Join(dir, "manga.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	works, err := s.ListWorks(context.Background())
	if err != nil || len(works) != 0 {
		t.Fatal(works, err)
	}
}

func TestAppDeleteWorkPendingStartupAndOtherAdmission(t *testing.T) {
	a := testApp(t, t.TempDir())
	for _, begin := range []func(int64) (func(), error){a.workOperations.Begin, a.workOperations.BeginEdit} {
		end, err := begin(3)
		if err != nil {
			t.Fatal(err)
		}
		_, err = a.DeleteWork(3, DeleteWorkInput{})
		end()
		editCode(t, err, "work_busy")
	}
	end, err := a.workOperations.BeginDelete(3)
	if err != nil {
		t.Fatal(err)
	}
	notices := 0
	a.deleteBusyNotice = func(context.Context) { notices++ }
	if !a.beforeClose(context.Background()) || notices != 1 || a.coverCleanupPhase.Load() != 0 {
		t.Fatal("refused close changed pending startup cleanup")
	}
	end()
	if a.beforeClose(context.Background()) {
		t.Fatal("close remained refused")
	}
}

type appDeleteRepositoryFunc func(context.Context, int64, manga.WorkDeleteInput) (manga.Work, error)

func (f appDeleteRepositoryFunc) DeleteWork(ctx context.Context, id int64, input manga.WorkDeleteInput) (manga.Work, error) {
	return f(ctx, id, input)
}

func TestAppDeleteWorkOutcomes(t *testing.T) {
	for _, tc := range []struct {
		cause     error
		code      string
		committed bool
	}{
		{errors.Join(storage.ErrWorkDeleteOutcomeUnknown, storage.ErrDBStopped), "work_delete_outcome_unknown", false},
		{errors.Join(storage.ErrWorkDeleteChanged, storage.ErrDBStopped), "database_stopped", false},
		{storage.ErrWorkDeleteChanged, "work_changed", false},
		{storage.ErrWorkNotFound, "work_not_found", false},
		{errors.New("private path or diagnostic"), "storage_error", false},
		{errors.Join(storage.ErrWorkDeleteCommitted, storage.ErrDBStopped), "", true},
	} {
		t.Run(tc.cause.Error(), func(t *testing.T) {
			a := testApp(t, t.TempDir())
			calls := 0
			a.deleter = application.NewWorkDeleter(appDeleteRepositoryFunc(func(context.Context, int64, manga.WorkDeleteInput) (manga.Work, error) {
				calls++
				return manga.Work{ID: 7}, tc.cause
			}), a.covers)
			result, err := a.DeleteWork(7, DeleteWorkInput{})
			if tc.committed {
				if err != nil || result.WorkID != 7 || len(result.Warnings) != 1 || result.Warnings[0].Code != "database_stopped_after_commit" {
					t.Fatal(result, err)
				}
			} else {
				editCode(t, err, tc.code)
				if result.WorkID != 0 || strings.Contains(formatAPIError(err).(string), "private path") {
					t.Fatal(result, err)
				}
			}
			if calls != 1 {
				t.Fatal("retried", calls)
			}
			end, err := a.workOperations.BeginDelete(7)
			if err != nil {
				t.Fatal("reservation leaked", err)
			}
			end()
		})
	}
}

type deleteCleanupRepository struct {
	application.CoverRepository
	entered chan struct{}
	release <-chan struct{}
}

func (r deleteCleanupRepository) UnusedManagedCover(ctx context.Context, _ string) (storage.ManagedCover, bool, error) {
	close(r.entered)
	select {
	case <-r.release:
	case <-ctx.Done():
		return storage.ManagedCover{}, false, ctx.Err()
	}
	return storage.ManagedCover{}, false, storage.ErrManagedCoverNotFound
}

func TestAppDeleteWorkAdmissionThroughCleanupAndClose(t *testing.T) {
	dir := t.TempDir()
	a := testApp(t, dir)
	if _, err := a.LoadCoverWarnings(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a.ctx = ctx
	w, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/cleanup", Title: "Cleanup", ThumbnailPath: "covers/" + strings.Repeat("a", 32) + ".png"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/other", Title: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	covers := application.NewCoverImporter(deleteCleanupRepository{CoverRepository: a.store, entered: entered, release: release}, &a.coverOperations, dir, nil, time.Now)
	a.deleter = application.NewWorkDeleter(a.store, covers)
	done := make(chan error, 1)
	go func() { _, err := a.DeleteWork(w.ID, deleteSnapshot(w)); done <- err }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// The row is already deleted, but the operation still owns its reservation.
	if _, err := a.store.WorkByID(ctx, w.ID); !errors.Is(err, storage.ErrWorkNotFound) {
		t.Fatal(err)
	}
	_, err = a.DeleteWork(w.ID, deleteSnapshot(w))
	editCode(t, err, "work_busy")
	_, err = a.EditWork(w.ID, EditWorkInput{})
	editCode(t, err, "work_busy")
	_, err = a.OpenWork(w.ID)
	editCode(t, err, "work_open_in_progress")
	_, err = a.SelectWorkCover(w.ID)
	editCode(t, err, "cover_busy")
	_, err = a.RemoveWorkCover(w.ID)
	editCode(t, err, "cover_busy")
	if _, err := a.EditWork(other.ID, EditWorkInput{URL: other.URL, Title: "Other changed"}); err != nil {
		t.Fatal(err)
	}
	notices := 0
	a.deleteBusyNotice = func(context.Context) { notices++ }
	if !a.beforeClose(ctx) || notices != 1 {
		t.Fatal("close not refused")
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if a.beforeClose(ctx) {
		t.Fatal("close after cleanup refused")
	}
	_, err = a.DeleteWork(other.ID, deleteSnapshot(other))
	editCode(t, err, "database_unavailable")
}
