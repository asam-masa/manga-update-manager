package application

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

func TestSelectionDoneReleasesOnlyChooser(t *testing.T) {
	a, _, work, _ := importFixture(t)
	ctx := context.Background()
	a.openSource = func(context.Context, string) (io.ReadCloser, error) { return nil, io.EOF }
	calls := 0
	_, err := a.SelectWithSelectionDone(ctx, work.ID, func() {
		calls++
		// The same work is still owned, but another chooser may be admitted.
		if _, err := a.operations.BeginChange(work.ID, true); !errors.Is(err, ErrCoverBusy) {
			t.Errorf("same work: %v", err)
		}
		other, err := a.operations.BeginChange(work.ID+1, true)
		if err != nil {
			t.Errorf("selection slot not released: %v", err)
			return
		}
		if err := other.End(); err != nil {
			t.Error(err)
		}
	})
	if !errors.Is(err, io.EOF) || calls != 1 {
		t.Fatal(calls, err)
	}
}

func TestSelectionDoneNotificationBoundaries(t *testing.T) {
	for _, test := range []struct {
		name      string
		path      string
		chooseErr error
	}{
		{"cancel", "", nil}, {"chooser failure", "", io.EOF},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, _, work, _ := importFixture(t)
			a.choose = func(context.Context) (string, error) { return test.path, test.chooseErr }
			calls := 0
			result, err := a.SelectWithSelectionDone(context.Background(), work.ID, func() { calls++ })
			if calls != 1 || !errors.Is(err, test.chooseErr) || result.Cancelled != (test.chooseErr == nil) {
				t.Fatal(result, calls, err)
			}
		})
	}
	a, _, work, _ := importFixture(t)
	ctx := context.Background()
	calls := 0
	notify := func() { calls++ }
	if _, err := a.SelectWithSelectionDone(ctx, work.ID+100, notify); !errors.Is(err, storage.ErrWorkNotFound) {
		t.Fatal(err)
	}
	op, err := a.operations.BeginChange(work.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SelectWithSelectionDone(ctx, work.ID+1, notify); !errors.Is(err, ErrCoverBusy) {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("notified without opening chooser", calls)
	}
	if err := op.End(); err != nil {
		t.Fatal(err)
	}
}

func TestSelectionDoneCallbacksBelongToRequests(t *testing.T) {
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
	firstNotified := make(chan struct{})
	sourceStarted := make(chan struct{})
	release := make(chan struct{})
	a.openSource = func(context.Context, string) (io.ReadCloser, error) {
		close(sourceStarted)
		<-release
		return nil, io.EOF
	}
	done := make(chan error, 1)
	defer func() {
		close(release)
		if err := <-done; !errors.Is(err, io.EOF) {
			t.Error(err)
		}
	}()
	go func() {
		_, err := a.SelectWithSelectionDone(ctx, work.ID, func() { close(firstNotified) })
		done <- err
	}()
	<-firstNotified
	<-sourceStarted
	// The first chooser has returned and only its source read is blocked.
	a.choose = func(context.Context) (string, error) { return "", nil }
	secondCalls := 0
	if result, err := a.SelectWithSelectionDone(ctx, other.ID, func() { secondCalls++ }); err != nil || !result.Cancelled {
		t.Fatal(result, err)
	}
	if secondCalls != 1 {
		t.Fatal("second request callback", secondCalls)
	}
}
