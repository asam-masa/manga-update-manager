package application

import (
	"context"

	"github.com/asam-masa/manga-update-manager/internal/storage"
)

type TagManagementReadRepository interface {
	ListTagUsage(context.Context) ([]storage.TagUsage, error)
	PreviewTagDelete(context.Context, int64) (storage.TagDeletePreview, error)
}

type TagManagementReader struct{ repository TagManagementReadRepository }

func NewTagManagementReader(repository TagManagementReadRepository) *TagManagementReader {
	return &TagManagementReader{repository: repository}
}

func (a *TagManagementReader) ListTagUsage(ctx context.Context) ([]storage.TagUsage, error) {
	return a.repository.ListTagUsage(ctx)
}

func (a *TagManagementReader) PreviewTagDelete(ctx context.Context, id int64) (storage.TagDeletePreview, error) {
	return a.repository.PreviewTagDelete(ctx, id)
}
