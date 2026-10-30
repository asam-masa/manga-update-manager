package application

import (
	"context"

	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

type TagChangeRepository interface {
	RenameTag(context.Context, manga.Tag, string) (manga.Tag, error)
	DeleteTag(context.Context, storage.TagDeletePreview) (manga.Tag, error)
}

type TagManager struct{ repository TagChangeRepository }

func NewTagManager(repository TagChangeRepository) *TagManager {
	return &TagManager{repository: repository}
}

func (a *TagManager) RenameTag(ctx context.Context, expected manga.Tag, value string) (manga.Tag, error) {
	name, err := manga.NormalizeTagName(value)
	if err != nil {
		return manga.Tag{}, err
	}
	return a.repository.RenameTag(ctx, expected, string(name))
}

func (a *TagManager) DeleteTag(ctx context.Context, expected storage.TagDeletePreview) (manga.Tag, error) {
	return a.repository.DeleteTag(ctx, expected)
}
