package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/manga"
)

type blockingWorkLookup struct {
	application.CoverRepository
	id      int64
	entered chan struct{}
	release chan struct{}
}

func (r blockingWorkLookup) WorkByID(ctx context.Context, id int64) (manga.Work, error) {
	if id == r.id {
		close(r.entered)
		select {
		case <-r.release:
		case <-ctx.Done():
			return manga.Work{}, ctx.Err()
		}
	}
	return r.CoverRepository.WorkByID(ctx, id)
}

func TestAppWorkOperationCrossExclusion(t *testing.T) {
	for _, first := range []string{"open", "select", "select-operation", "remove", "remove-operation"} {
		t.Run(first, func(t *testing.T) {
			directory := t.TempDir()
			a := testApp(t, directory)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			a.ctx = ctx
			if _, err := a.LoadCoverWarnings(); err != nil {
				t.Fatal(err)
			}
			w, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/one", Title: "One"})
			if err != nil {
				t.Fatal(err)
			}
			other, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/two", Title: "Two"})
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
			a.coverSelectionFinished = func(context.Context, int64, string) {}
			a.opener = application.NewWorkOpener(a.store, func(url string) error {
				if first == "open" && url == w.URL {
					close(entered)
					select {
					case <-release:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				return nil
			}, time.Now)
			var repo application.CoverRepository = a.store
			if strings.HasPrefix(first, "remove") {
				repo = blockingWorkLookup{a.store, w.ID, entered, release}
			}
			a.covers = application.NewCoverImporter(repo, &a.coverOperations, directory, func(context.Context) (string, error) {
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
					return "", ctx.Err()
				}
				return "", nil
			}, time.Now)
			done := make(chan error, 1)
			go func() {
				var err error
				switch first {
				case "open":
					_, err = a.OpenWork(w.ID)
				case "select":
					_, err = a.SelectWorkCover(w.ID)
				case "select-operation":
					_, err = a.SelectWorkCoverWithOperation(w.ID, strings.Repeat("a", 32))
				case "remove":
					_, err = a.RemoveWorkCover(w.ID)
				case "remove-operation":
					_, err = a.RemoveWorkCoverWithOperation(w.ID, strings.Repeat("a", 32))
				}
				done <- err
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if _, err := a.OpenWork(w.ID); errorCode(t, err) != "work_open_in_progress" {
				t.Fatal(err)
			}
			if _, err := a.SelectWorkCover(w.ID); errorCode(t, err) != "cover_busy" {
				t.Fatal(err)
			}
			if _, err := a.RemoveWorkCover(w.ID); errorCode(t, err) != "cover_busy" {
				t.Fatal(err)
			}
			if _, err := a.workOperations.BeginEdit(w.ID); !errors.Is(err, application.ErrWorkBusy) {
				t.Fatal(err)
			}
			if _, err := a.EditWork(w.ID, EditWorkInput{URL: w.URL, Title: w.Title}); errorCode(t, err) != "work_busy" {
				t.Fatal(err)
			}
			if _, err := a.OpenWork(other.ID); err != nil {
				t.Fatal("other work blocked", err)
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
			end, err := a.workOperations.BeginEdit(w.ID)
			if err != nil {
				t.Fatal("reservation leaked", err)
			}
			end()
		})
	}
}

func TestAppEditAdmissionClosePolicy(t *testing.T) {
	a := testApp(t, t.TempDir())
	notices := 0
	a.workBusyNotice = func(context.Context) { notices++ }
	end, err := a.workOperations.BeginEdit(1)
	if err != nil {
		t.Fatal(err)
	}
	if !a.beforeClose(context.Background()) || notices != 1 {
		t.Fatal("edit close not rejected", notices)
	}
	if a.coverCleanupPhase.Load() != 0 {
		t.Fatal("refused close discarded pending cleanup")
	}
	other, err := a.workOperations.Begin(2)
	if err != nil {
		t.Fatal("refusal closed gate", err)
	}
	other()
	end()
	if a.beforeClose(context.Background()) {
		t.Fatal("close rejected after result")
	}
	if _, err := a.workOperations.Begin(1); !errors.Is(err, application.ErrWorkClosing) {
		t.Fatal(err)
	}
	if _, err := a.OpenWork(1); errorCode(t, err) != "database_unavailable" {
		t.Fatal(err)
	}
	if _, err := a.SelectWorkCover(1); errorCode(t, err) != "cover_unavailable" {
		t.Fatal(err)
	}
}

func TestAppWorkReservationReleaseAndImageOnlyStop(t *testing.T) {
	directory := t.TempDir()
	a := testApp(t, directory)
	if _, err := a.LoadCoverWarnings(); err != nil {
		t.Fatal(err)
	}
	w, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/failure", Title: "Failure"})
	if err != nil {
		t.Fatal(err)
	}
	a.covers = application.NewCoverImporter(a.store, &a.coverOperations, directory, func(context.Context) (string, error) {
		return "", errors.New("synthetic selection failure")
	}, time.Now)
	if _, err := a.SelectWorkCover(w.ID); err == nil {
		t.Fatal("expected failure")
	}
	end, err := a.workOperations.BeginEdit(w.ID)
	if err != nil {
		t.Fatal("failure leaked reservation", err)
	}
	end()
	a.coverOperations.Stop()
	if _, err := a.SelectWorkCover(w.ID); errorCode(t, err) != "cover_unavailable" {
		t.Fatal(err)
	}
	end, err = a.workOperations.BeginEdit(w.ID)
	if err != nil {
		t.Fatal("image-only stop disabled work admission", err)
	}
	end()
	if _, err := a.ListWorks(); err != nil {
		t.Fatal("image-only stop disabled DB", err)
	}
}
