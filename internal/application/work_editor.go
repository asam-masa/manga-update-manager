package application

import (
	"context"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
)

type WorkEditRepository interface {
	EditWork(context.Context, int64, manga.EditWorkInput, time.Time) (manga.Work, error)
}

type WorkEditor struct {
	repository WorkEditRepository
	now        func() time.Time
}

func NewWorkEditor(repository WorkEditRepository, now func() time.Time) *WorkEditor {
	return &WorkEditor{repository: repository, now: now}
}

func (a *WorkEditor) EditWork(ctx context.Context, id int64, input manga.EditWorkInput) (manga.Work, error) {
	input, err := input.Normalize()
	if err != nil {
		return manga.Work{}, err
	}
	return a.repository.EditWork(ctx, id, input, a.now())
}
