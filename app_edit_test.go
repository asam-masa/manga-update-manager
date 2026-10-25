package main

import (
	"context"
	"errors"
	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/storage"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type appEditRepositoryFunc func(context.Context, int64, manga.EditWorkInput, time.Time) (manga.Work, error)

func (f appEditRepositoryFunc) EditWork(ctx context.Context, id int64, input manga.EditWorkInput, at time.Time) (manga.Work, error) {
	return f(ctx, id, input, at)
}

func editCode(t *testing.T, err error, code string) {
	t.Helper()
	var api *APIError
	if !errors.As(err, &api) || api.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

func TestAppEditWorkPersistence(t *testing.T) {
	directory := t.TempDir()
	a := testApp(t, directory)
	a.opener = application.NewWorkOpener(a.store, func(string) error { return nil }, time.Now)
	w, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/old", Title: "Old", ThumbnailPath: "covers/existing.png"})
	if err != nil {
		t.Fatal(err)
	}
	w, err = a.OpenWork(w.ID)
	if err != nil {
		t.Fatal(err)
	}
	input := EditWorkInput{URL: " https://example.com/new ", Title: " New ", SiteName: " Site ", Notes: " Notes "}
	result, err := a.EditWork(w.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Warnings == nil || len(result.Warnings) != 0 || result.Work.URL != "https://example.com/new" || result.Work.Title != "New" || result.Work.SiteName != "Site" || result.Work.Notes != "Notes" {
		t.Fatalf("result %+v", result)
	}
	got := result.Work
	if got.ID != w.ID || got.CreatedAt != w.CreatedAt || got.ThumbnailPath != w.ThumbnailPath || !reflect.DeepEqual(got.LastAccessedAt, w.LastAccessedAt) || !strings.HasSuffix(got.UpdatedAt, "+09:00") {
		t.Fatalf("lost fields: %+v", got)
	}
	same, err := a.EditWork(w.ID, input)
	if err != nil || !reflect.DeepEqual(same, result) {
		t.Fatalf("no change: %+v %v", same, err)
	}
	_, err = a.CreateWork(CreateWorkInput{URL: "https://example.com/other", Title: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id    int64
		input EditWorkInput
		code  string
	}{
		{w.ID, EditWorkInput{}, "invalid_input"},
		{w.ID, EditWorkInput{URL: "https://example.com/other", Title: "Duplicate"}, "duplicate_url"},
		{999, input, "work_not_found"},
	} {
		_, err := a.EditWork(tc.id, tc.input)
		editCode(t, err, tc.code)
	}
	a.coverOperations.Stop()
	if _, err := a.EditWork(w.ID, input); err != nil {
		t.Fatal("image-only stop affected edit", err)
	}
	a.shutdown(context.Background())
	_, err = a.EditWork(w.ID, input)
	editCode(t, err, "database_unavailable")
	s, err := storage.Open(context.Background(), filepath.Join(directory, "manga.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	stored, err := s.WorkByID(context.Background(), w.ID)
	if err != nil || !reflect.DeepEqual(workDTO(stored), result.Work) {
		t.Fatalf("reopen: %+v %v", stored, err)
	}
}

func TestAppEditWorkOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name      string
		cause     error
		code      string
		committed bool
	}{
		{"unknown", errors.Join(storage.ErrWorkEditOutcomeUnknown, storage.ErrDBStopped), "work_edit_outcome_unknown", false},
		{"committed", errors.Join(storage.ErrWorkEditCommitted, storage.ErrDBStopped), "", true},
		{"stopped", storage.ErrDBStopped, "database_stopped", false},
		{"ordinary", errors.New("private diagnostic"), "storage_error", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testApp(t, t.TempDir())
			calls := 0
			w, err := manga.NewWork(manga.NewWorkInput{URL: "https://example.com/", Title: "Title"}, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			w.ID = 1
			w.ScheduledDate = "2026-10-30"
			a.editor = application.NewWorkEditor(appEditRepositoryFunc(func(context.Context, int64, manga.EditWorkInput, time.Time) (manga.Work, error) {
				calls++
				return w, tc.cause
			}), time.Now)
			result, err := a.EditWork(1, EditWorkInput{URL: w.URL, Title: w.Title})
			if tc.committed {
				if err != nil || result.Work.ID != 1 || len(result.Warnings) != 1 || result.Warnings[0].Code != "database_stopped_after_commit" {
					t.Fatalf("committed: %+v %v", result, err)
				}
				if result.Work.ScheduledDate == nil || *result.Work.ScheduledDate != "2026-10-30" {
					t.Fatal("committed date missing", result)
				}
			} else {
				editCode(t, err, tc.code)
				wire := formatAPIError(err).(string)
				if strings.Contains(wire, "private diagnostic") || !strings.Contains(wire, tc.code) || result.Work.ID != 0 {
					t.Fatalf("unsafe result %s", wire)
				}
			}
			if calls != 1 {
				t.Fatal("retried")
			}
			end, err := a.workOperations.BeginEdit(1)
			if err != nil {
				t.Fatal(err)
			}
			end()
		})
	}
}

func TestAppEditWorkAdmissionAndClose(t *testing.T) {
	a := testApp(t, t.TempDir())
	if _, err := a.LoadCoverWarnings(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a.ctx = ctx
	entered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	a.editor = application.NewWorkEditor(appEditRepositoryFunc(func(ctx context.Context, id int64, _ manga.EditWorkInput, _ time.Time) (manga.Work, error) {
		if id == 1 {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return manga.Work{}, ctx.Err()
			}
		}
		return manga.Work{ID: id}, nil
	}), time.Now)
	input := EditWorkInput{URL: "https://example.com/", Title: "Title"}
	done := make(chan error, 1)
	go func() { _, err := a.EditWork(1, input); done <- err }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	_, err := a.EditWork(1, input)
	editCode(t, err, "work_busy")
	_, err = a.OpenWork(1)
	editCode(t, err, "work_open_in_progress")
	_, err = a.RemoveWorkCover(1)
	editCode(t, err, "cover_busy")
	_, err = a.SelectWorkCover(1)
	editCode(t, err, "cover_busy")
	if _, err := a.EditWork(2, input); err != nil {
		t.Fatal(err)
	}
	notices := 0
	a.workBusyNotice = func(context.Context) { notices++ }
	if !a.beforeClose(ctx) || notices != 1 {
		t.Fatal("close did not refuse")
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
		t.Fatal("close after edit refused")
	}
	_, err = a.EditWork(2, input)
	editCode(t, err, "database_unavailable")
}
