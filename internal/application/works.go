package application

import (
	"context"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
)

// WorkRepository is the persistence boundary required by these operations.
type WorkRepository interface {
	CreateWork(context.Context, manga.Work) (manga.Work, error)
	ListWorks(context.Context) ([]manga.Work, error)
}

type Works struct {
	repository WorkRepository
	now        func() time.Time
}

func NewWorks(repository WorkRepository, now func() time.Time) *Works {
	return &Works{repository: repository, now: now}
}

func (a *Works) CreateWork(ctx context.Context, input manga.NewWorkInput) (manga.Work, error) {
	work, err := manga.NewWork(input, a.now())
	if err != nil {
		return manga.Work{}, err
	}
	return a.repository.CreateWork(ctx, work)
}

func (a *Works) ListWorks(ctx context.Context) ([]manga.Work, error) {
	return a.repository.ListWorks(ctx)
}
