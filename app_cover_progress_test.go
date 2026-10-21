package main

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/manga"
)

type progressRepository struct {
	application.CoverRepository
	beforeUpdate func() error
}

func (r progressRepository) SetWorkCover(ctx context.Context, workID int64, id string, now time.Time) (manga.Work, string, error) {
	if err := r.beforeUpdate(); err != nil {
		return manga.Work{}, "", err
	}
	return r.CoverRepository.SetWorkCover(ctx, workID, id, now)
}

func (r progressRepository) RemoveWorkCover(ctx context.Context, workID int64, now time.Time) (manga.Work, string, error) {
	if err := r.beforeUpdate(); err != nil {
		return manga.Work{}, "", err
	}
	return r.CoverRepository.RemoveWorkCover(ctx, workID, now)
}

func TestCoverProgressDuringRealReferenceUpdate(t *testing.T) {
	for _, kind := range []string{"select", "remove", "failure"} {
		t.Run(kind, func(t *testing.T) {
			a, dir, _, work := coverStopFixture(t)
			id := strings.Repeat("c", 32)
			calls := 0
			repository := progressRepository{a.store, func() error {
				calls++
				got := a.LoadCoverProgress()
				if len(got) != 1 || got[0].OperationID != id || got[0].WorkID != work.ID || !got[0].Timed || got[0].Stage != "updating" {
					t.Error(got)
				}
				data, err := json.Marshal(got)
				if err != nil || strings.Contains(string(data), dir) || strings.Contains(string(data), "source.png") {
					t.Error(string(data), err)
				}
				if kind == "failure" {
					return io.EOF
				}
				return nil
			}}
			a.covers = application.NewCoverImporter(repository, &a.coverOperations, dir, func(context.Context) (string, error) { return filepath.Join(dir, "source.png"), nil }, time.Now)
			a.coverSelectionFinished = func(context.Context, int64, string) {}
			var err error
			if kind == "remove" {
				_, err = a.RemoveWorkCoverWithOperation(work.ID, id)
			} else {
				_, err = a.SelectWorkCoverWithOperation(work.ID, id)
			}
			if (kind == "failure") != (err != nil) || calls != 1 || len(a.LoadCoverProgress()) != 0 {
				t.Fatal(calls, err, a.LoadCoverProgress())
			}
		})
	}
}

func TestCoverProgressTimingIdentityAndCopies(t *testing.T) {
	a := NewApp()
	now := time.Now()
	a.coverProgress.now = func() time.Time { return now }
	id := strings.Repeat("a", 32)
	notify, finish, ok := a.coverProgress.begin(id, 7, "select")
	if !ok || len(a.LoadCoverProgress()) != 0 {
		t.Fatal("unobserved request published")
	}
	notify(application.CoverProgressChoosing)
	now = now.Add(time.Minute)
	if got := a.LoadCoverProgress(); len(got) != 1 || got[0].Timed || got[0].ElapsedMilliseconds != 0 {
		t.Fatal(got)
	}
	_, duplicateFinish, ok := a.coverProgress.begin(id, 8, "remove")
	duplicateFinish()
	if ok || len(a.LoadCoverProgress()) != 1 {
		t.Fatal("duplicate replaced or retired first request")
	}
	notify(application.CoverProgressGenerating)
	now = now.Add(1234 * time.Millisecond)
	notify(application.CoverProgressReleasing)
	got := a.LoadCoverProgress()
	want := CoverProgressDTO{OperationID: id, WorkID: 7, Kind: "select", Stage: "releasing", Timed: true, ElapsedMilliseconds: 1234}
	if len(got) != 1 || got[0] != want {
		t.Fatal(got, want)
	}
	got[0].Stage = "caller mutation"
	if a.LoadCoverProgress()[0] != want {
		t.Fatal("mutable snapshot")
	}
	finish()
	// A stale callback/finish may not change a reused ID's new entry.
	other, otherFinish, ok := a.coverProgress.begin(id, 8, "remove")
	if !ok {
		t.Fatal("completed entry retained")
	}
	other(application.CoverProgressChecking)
	now = now.Add(2 * time.Second)
	notify(application.CoverProgressSaving)
	finish()
	if got := a.LoadCoverProgress(); len(got) != 1 || got[0].Stage != "checking" || got[0].ElapsedMilliseconds != 2000 {
		t.Fatal(got)
	}
	otherFinish()
	encoded, err := json.Marshal(a.LoadCoverProgress())
	if err != nil || string(encoded) != "[]" {
		t.Fatal(string(encoded), err)
	}
}

func TestCoverProgressReadDoesNotWaitForAppLifetime(t *testing.T) {
	a := NewApp()
	a.mu.Lock()
	done := make(chan []CoverProgressDTO, 1)
	go func() { done <- a.LoadCoverProgress() }()
	select {
	case got := <-done:
		if len(got) != 0 {
			t.Error(got)
		}
	case <-time.After(5 * time.Second):
		t.Error("progress waited for App lifetime lock")
	}
	a.mu.Unlock()
}

func TestCoverProgressAPICancelRejectAndLegacy(t *testing.T) {
	a := NewApp()
	a.dataDirectory = func() (string, error) { return t.TempDir(), nil }
	var id string
	a.chooseCover = func(context.Context) (string, error) {
		if id != "" {
			got := a.LoadCoverProgress()
			if len(got) != 1 || got[0].OperationID != id || got[0].Stage != "choosing" || got[0].Timed {
				t.Error(got)
			}
			if _, err := a.RemoveWorkCoverWithOperation(2, id); errorCode(t, err) != "cover_busy" {
				t.Error(err)
			}
			if len(a.LoadCoverProgress()) != 1 {
				t.Error("duplicate retired first request")
			}
		} else if len(a.LoadCoverProgress()) != 0 {
			t.Error("legacy API published progress")
		}
		return "", nil
	}
	a.coverSelectionFinished = func(context.Context, int64, string) {}
	a.startup(context.Background())
	defer a.shutdown(context.Background())
	if _, err := a.LoadCoverWarnings(); err != nil {
		t.Fatal(err)
	}
	work, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/progress", Title: "Synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"", "path/private", strings.Repeat("A", 32)} {
		if _, err := a.RemoveWorkCoverWithOperation(work.ID, invalid); errorCode(t, err) != "invalid_input" {
			t.Fatal(err)
		}
	}
	id = strings.Repeat("a", 32)
	result, err := a.SelectWorkCoverWithOperation(work.ID, id)
	if err != nil || !result.Cancelled || len(a.LoadCoverProgress()) != 0 {
		t.Fatal(result, err)
	}
	if _, err := a.SelectWorkCoverWithOperation(work.ID+99, id); errorCode(t, err) != "work_not_found" || len(a.LoadCoverProgress()) != 0 {
		t.Fatal(err)
	}
	id = ""
	if _, err := a.SelectWorkCover(work.ID); err != nil {
		t.Fatal(err)
	}
	removed, err := a.RemoveWorkCoverWithOperation(work.ID, strings.Repeat("b", 32))
	if err != nil || removed.Work.ID != work.ID || len(a.LoadCoverProgress()) != 0 {
		t.Fatal(removed, err)
	}
}

func TestCoverProgressConcurrentReadAndFinish(t *testing.T) {
	a := NewApp()
	var wg sync.WaitGroup
	for _, id := range []string{strings.Repeat("a", 32), strings.Repeat("b", 32)} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			notify, finish, ok := a.coverProgress.begin(id, 1, "remove")
			if !ok {
				t.Error("independent ID rejected")
				return
			}
			defer finish()
			for range 100 {
				notify(application.CoverProgressChecking)
				_ = a.LoadCoverProgress()
			}
		}()
	}
	wg.Wait()
	if !reflect.DeepEqual(a.LoadCoverProgress(), []CoverProgressDTO{}) {
		t.Fatal(a.LoadCoverProgress())
	}
}
