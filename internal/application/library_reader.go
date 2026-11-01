package application

import (
	"context"

	"github.com/asam-masa/manga-update-manager/internal/storage"
)

type LibraryReadRepository interface {
	LoadLibrarySnapshot(context.Context) (storage.LibrarySnapshot, error)
}

type LibraryReader struct{ repository LibraryReadRepository }

func NewLibraryReader(repository LibraryReadRepository) *LibraryReader {
	return &LibraryReader{repository: repository}
}

func (a *LibraryReader) LoadLibrarySnapshot(ctx context.Context) (storage.LibrarySnapshot, error) {
	return a.repository.LoadLibrarySnapshot(ctx)
}
