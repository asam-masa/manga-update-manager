package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
)

type fakeRepository struct {
	called bool
	work   manga.Work
	err    error
}

func (f *fakeRepository) CreateWork(_ context.Context, work manga.Work) (manga.Work, error) {
	f.called = true
	f.work = work
	work.ID = 7
	return work, f.err
}
func (f *fakeRepository) ListWorks(context.Context) ([]manga.Work, error) {
	return []manga.Work{f.work}, f.err
}

func TestCreateWorkValidatesBeforePersistence(t *testing.T) {
	repository := &fakeRepository{}
	now := time.Date(2026, 10, 5, 9, 0, 0, 123, time.FixedZone("JST", 9*60*60))
	app := NewWorks(repository, func() time.Time { return now })
	if _, err := app.CreateWork(context.Background(), manga.NewWorkInput{}); !errors.Is(err, manga.ErrURLRequired) || repository.called {
		t.Fatalf("invalid input reached persistence: %v", err)
	}
	work, err := app.CreateWork(context.Background(), manga.NewWorkInput{URL: " https://example.com/1 ", Title: " 作品 "})
	if err != nil || work.ID != 7 || work.Title != "作品" || work.URL != "https://example.com/1" || work.CreatedAt != now.UTC() || work.UpdatedAt != work.CreatedAt {
		t.Fatalf("CreateWork = %#v, %v", work, err)
	}
	repository.err = manga.ErrDuplicateURL
	if _, err := app.CreateWork(context.Background(), manga.NewWorkInput{URL: work.URL, Title: work.Title}); !errors.Is(err, manga.ErrDuplicateURL) {
		t.Fatalf("duplicate = %v", err)
	}
	if _, err := app.ListWorks(context.Background()); !errors.Is(err, manga.ErrDuplicateURL) {
		t.Fatalf("repository failure = %v", err)
	}
}
