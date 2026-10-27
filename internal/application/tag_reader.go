package application

import (
	"context"

	"github.com/asam-masa/manga-update-manager/internal/manga"
)

// TagReadRepository contains only the queries this consumer needs.
type TagReadRepository interface {
	ListTags(context.Context) ([]manga.Tag, error)
	WorkTags(context.Context, int64) ([]manga.Tag, error)
}

type TagReader struct{ repository TagReadRepository }

func NewTagReader(repository TagReadRepository) *TagReader {
	return &TagReader{repository: repository}
}

func (a *TagReader) ListTags(ctx context.Context) ([]manga.Tag, error) {
	return a.repository.ListTags(ctx)
}

func (a *TagReader) WorkTags(ctx context.Context, id int64) ([]manga.Tag, error) {
	return a.repository.WorkTags(ctx, id)
}
