package application

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/platform"
)

func TestCoverProgressSelectRemoveAndCleanup(t *testing.T) {
	a, store, work, _ := importFixture(t)
	ctx := context.Background()
	if _, err := a.Select(ctx, work.ID); err != nil {
		t.Fatal(err)
	}
	var got []CoverProgressStage
	notify := func(stage CoverProgressStage) { got = append(got, stage) }
	// Verify that observation precedes actual I/O, including deferred release.
	open, prepare, write := a.openSource, a.prepare, a.write
	a.openSource = func(ctx context.Context, path string) (io.ReadCloser, error) {
		if got[len(got)-1] != CoverProgressGenerating {
			t.Error("source opened before notification")
		}
		return open(ctx, path)
	}
	a.prepare = func(ctx context.Context, dir string) (io.Closer, error) {
		if got[len(got)-1] != CoverProgressPreparing {
			t.Error("directory prepared before notification")
		}
		guard, err := prepare(ctx, dir)
		if err != nil {
			return nil, err
		}
		return importCloser{func() error {
			if got[len(got)-1] != CoverProgressReleasing {
				t.Error("guard closed before notification")
			}
			return guard.Close()
		}}, nil
	}
	a.write = func(ctx context.Context, dir, id string, png []byte) (platform.CoverFileInfo, error) {
		if got[len(got)-1] != CoverProgressSaving {
			t.Error("file written before notification")
		}
		return write(ctx, dir, id, png)
	}
	selected := false
	result, err := a.SelectWithProgress(ctx, work.ID, func() { selected = true }, func(stage CoverProgressStage) {
		if stage == CoverProgressGenerating && !selected {
			t.Error("generation notified before chooser completion")
		}
		notify(stage)
	})
	want := []CoverProgressStage{CoverProgressChecking, CoverProgressChoosing, CoverProgressGenerating, CoverProgressReleasing,
		CoverProgressPreparing, CoverProgressSaving, CoverProgressSaving, CoverProgressWaiting, CoverProgressUpdating,
		CoverProgressUpdating, CoverProgressCleaning, CoverProgressCleaning, CoverProgressCleaning, CoverProgressReleasing}
	if err != nil || result.Work == nil || len(result.Warnings) != 0 || !reflect.DeepEqual(got, want) {
		t.Fatal(got, result, err)
	}
	got = nil
	removed, err := a.RemoveWithProgress(ctx, work.ID, notify)
	want = []CoverProgressStage{CoverProgressChecking, CoverProgressPreparing, CoverProgressWaiting, CoverProgressUpdating,
		CoverProgressCleaning, CoverProgressCleaning, CoverProgressCleaning, CoverProgressReleasing}
	if err != nil || removed.Work == nil || removed.Work.ThumbnailPath != "" || !reflect.DeepEqual(got, want) {
		t.Fatal(got, removed, err)
	}
	// Leave an unreferenced managed image, then observe startup cleanup.
	a.openSource, a.prepare, a.write = open, prepare, write
	if _, err := a.Select(ctx, work.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.RemoveWorkCover(ctx, work.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	op, err := a.operations.BeginCleanup()
	if err != nil {
		t.Fatal(err)
	}
	got = nil
	warnings, err := a.CleanupWithProgress(ctx, op, notify)
	want = []CoverProgressStage{CoverProgressWaiting, CoverProgressChecking, CoverProgressChecking, CoverProgressPreparing,
		CoverProgressCleaning, CoverProgressCleaning, CoverProgressCleaning, CoverProgressReleasing}
	if err != nil || len(warnings) != 0 || !reflect.DeepEqual(got, want) {
		t.Fatal(got, warnings, err)
	}
	if records, err := store.ListManagedCovers(ctx); err != nil || len(records) != 0 {
		t.Fatal(records, err)
	}
}

func TestCoverProgressFailureAndCancellation(t *testing.T) {
	for _, mode := range []string{"cancel", "chooser-error", "source-error", "source-release-error", "stop-after-chooser", "release-warning"} {
		t.Run(mode, func(t *testing.T) {
			a, _, work, _ := importFixture(t)
			synthetic := errors.New("synthetic failure")
			want := []CoverProgressStage{CoverProgressChecking, CoverProgressChoosing}
			switch mode {
			case "cancel":
				a.choose = func(context.Context) (string, error) { return "", nil }
			case "chooser-error":
				a.choose = func(context.Context) (string, error) { return "", synthetic }
			case "source-error":
				a.openSource = func(context.Context, string) (io.ReadCloser, error) { return nil, synthetic }
				want = append(want, CoverProgressGenerating)
			case "source-release-error":
				open := a.openSource
				a.openSource = func(ctx context.Context, path string) (io.ReadCloser, error) {
					source, err := open(ctx, path)
					if err != nil {
						return nil, err
					}
					return importSource{source}, nil
				}
				want = append(want, CoverProgressGenerating, CoverProgressReleasing)
			case "stop-after-chooser":
				a.choose = func(context.Context) (string, error) { a.operations.Stop(); return "source", nil }
			case "release-warning":
				prepare := a.prepare
				a.prepare = func(ctx context.Context, dir string) (io.Closer, error) {
					guard, err := prepare(ctx, dir)
					if err != nil {
						return nil, err
					}
					return importCloser{func() error { return errors.Join(guard.Close(), synthetic) }}, nil
				}
			}
			var got []CoverProgressStage
			result, err := a.SelectWithProgress(context.Background(), work.ID, nil, func(stage CoverProgressStage) { got = append(got, stage) })
			if mode == "release-warning" {
				if err != nil || result.Work == nil || got[len(got)-1] != CoverProgressReleasing {
					t.Fatal(got, result, err)
				}
				found := false
				for _, code := range result.Warnings {
					if code == "cover_release_unconfirmed" {
						found = true
					}
				}
				if !found || !a.operations.stopped {
					t.Fatal("lost committed release warning", result)
				}
				return
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatal(got, want)
			}
			if mode == "cancel" {
				if err != nil || !result.Cancelled {
					t.Fatal(result, err)
				}
			} else if mode == "stop-after-chooser" {
				if !errors.Is(err, ErrCoverStopped) {
					t.Fatal(err)
				}
			} else if mode == "source-release-error" {
				if !errors.Is(err, platform.ErrCoverRelease) || result.Work != nil {
					t.Fatal(result, err)
				}
			} else if !errors.Is(err, synthetic) {
				t.Fatal(err)
			}
		})
	}
}

func TestCoverProgressRejectedStagesDoNotNotify(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		a, _, work, _ := importFixture(t)
		op, err := a.operations.BeginChange(work.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		if stopped {
			a.operations.Stop()
		} else {
			cancel()
		}
		called := false
		err = a.progressStage(ctx, op, func(CoverProgressStage) { called = true }, CoverProgressSaving, func() error { called = true; return nil })
		cancel()
		if err == nil || called {
			t.Fatal("rejected stage observed or run", err)
		}
		if err := op.End(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCoverProgressCallbacksBelongToConcurrentRequests(t *testing.T) {
	a, store, work, _ := importFixture(t)
	ctx := context.Background()
	other, err := manga.NewWork(manga.NewWorkInput{URL: "https://example.com/other", Title: "Other"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	other, err = store.CreateWork(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	a.openSource = func(context.Context, string) (io.ReadCloser, error) { close(entered); <-release; return nil, io.EOF }
	var first, second []CoverProgressStage
	go func() {
		_, err := a.SelectWithProgress(ctx, work.ID, nil, func(stage CoverProgressStage) { first = append(first, stage) })
		done <- err
	}()
	<-entered
	defer func() {
		close(release)
		if err := <-done; !errors.Is(err, io.EOF) {
			t.Error(err)
		}
	}()
	result, err := a.RemoveWithProgress(ctx, other.ID, func(stage CoverProgressStage) { second = append(second, stage) })
	if err != nil || result.Work == nil {
		t.Fatal(result, err)
	}
	if !reflect.DeepEqual(first, []CoverProgressStage{CoverProgressChecking, CoverProgressChoosing, CoverProgressGenerating}) {
		t.Fatal(first)
	}
	if !reflect.DeepEqual(second, []CoverProgressStage{CoverProgressChecking, CoverProgressPreparing, CoverProgressWaiting, CoverProgressUpdating, CoverProgressReleasing}) {
		t.Fatal(second)
	}
}
