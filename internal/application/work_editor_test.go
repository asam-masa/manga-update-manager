package application

import (
	"context"
	"errors"
	"github.com/asam-masa/manga-update-manager/internal/manga"
	"testing"
	"time"
)

type editRepositoryFunc func(context.Context, int64, manga.EditWorkInput, time.Time) (manga.Work, error)

func (f editRepositoryFunc) EditWork(ctx context.Context, id int64, input manga.EditWorkInput, at time.Time) (manga.Work, error) {
	return f(ctx, id, input, at)
}

func TestWorkEditorValidationAndForwarding(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 10, 22, 1, 2, 3, 0, time.UTC)
	wantErr := errors.New("repository failure")
	calls, clocks := 0, 0
	editor := NewWorkEditor(editRepositoryFunc(func(got context.Context, id int64, input manga.EditWorkInput, clock time.Time) (manga.Work, error) {
		calls++
		if got != ctx || id != 7 || clock != at || input != (manga.EditWorkInput{URL: "https://example.com/", Title: "Title", SiteName: "Site", Notes: "Notes"}) {
			t.Fatal("incorrect forwarding")
		}
		return manga.Work{ID: 7}, wantErr
	}), func() time.Time { clocks++; return at })
	if _, err := editor.EditWork(ctx, 7, manga.EditWorkInput{}); !errors.Is(err, manga.ErrURLRequired) || calls != 0 || clocks != 0 {
		t.Fatalf("invalid input: %v", err)
	}
	w, err := editor.EditWork(ctx, 7, manga.EditWorkInput{URL: " https://example.com/ ", Title: " Title ", SiteName: " Site ", Notes: " Notes "})
	if w.ID != 7 || !errors.Is(err, wantErr) || calls != 1 || clocks != 1 {
		t.Fatalf("result: %+v %v", w, err)
	}
}
