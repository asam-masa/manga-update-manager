package application

import (
	"context"
	"errors"
	"github.com/asam-masa/manga-update-manager/internal/manga"
	"testing"
	"time"
)

type accessFake struct {
	work             manga.Work
	readErr, saveErr error
	saved            []time.Time
}

func (f *accessFake) WorkByID(context.Context, int64) (manga.Work, error) { return f.work, f.readErr }
func (f *accessFake) RecordLastAccess(_ context.Context, _ int64, at time.Time) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, at)
	return nil
}

func TestOpenWorkOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		readErr, openErr, saveErr, want error
	}{
		{name: "success"}, {name: "missing", readErr: manga.ErrWorkNotFound, want: manga.ErrWorkNotFound},
		{name: "browser failure", openErr: errors.New("private detail"), want: ErrBrowserOpen},
		{name: "save failure", saveErr: errors.New("private detail"), want: ErrAccessSave},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Date(2026, 10, 9, 1, 2, 3, 123, time.UTC)
			work, _ := manga.NewWork(manga.NewWorkInput{URL: "https://example.com/work?chapter=1", Title: "作品"}, at.Add(-time.Hour))
			f := &accessFake{work: work, readErr: tc.readErr, saveErr: tc.saveErr}
			launched := false
			a := NewWorkOpener(f, func(url string) error {
				if url != work.URL {
					t.Fatal(url)
				}
				launched = true
				return tc.openErr
			}, func() time.Time {
				if !launched {
					t.Fatal("clock read before launch")
				}
				return at
			})
			got, err := a.OpenWork(context.Background(), 1)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error %v want %v", err, tc.want)
			}
			if tc.want == nil {
				if got.LastAccessedAt == nil || !got.LastAccessedAt.Equal(at) || len(f.saved) != 1 {
					t.Fatal(got)
				}
			} else if len(f.saved) != 0 {
				t.Fatal("failure saved access")
			}
			if tc.readErr != nil && launched {
				t.Fatal("opened missing work")
			}
		})
	}
}

func TestOpenWorkPreventsConcurrentLaunchAndAllowsRetry(t *testing.T) {
	at := time.Now().UTC()
	work, _ := manga.NewWork(manga.NewWorkInput{URL: "https://example.com", Title: "作品"}, at)
	f := &accessFake{work: work}
	entered, release := make(chan struct{}, 2), make(chan struct{})
	a := NewWorkOpener(f, func(string) error { entered <- struct{}{}; <-release; return nil }, func() time.Time { return at })
	done := make(chan error, 1)
	go func() { _, err := a.OpenWork(context.Background(), 1); done <- err }()
	<-entered
	if _, err := a.OpenWork(context.Background(), 1); !errors.Is(err, ErrOpenInProgress) {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := a.OpenWork(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
}
