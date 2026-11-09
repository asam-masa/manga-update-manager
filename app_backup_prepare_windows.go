package main

import (
	"context"
	"io"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

// The caller exclusively owns the held file, including later cleanup, Close and
// publication. Even successful preparation is not permission to publish after
// normal admission resumes; the final save coordinator must retain reservation.
type backupPrepareFile interface {
	io.Writer
	io.ReaderAt
	Sync() error
	Size() (int64, error)
	VerifyIdentity() error
}

func (a *App) prepareBackupArchive(ctx context.Context, file backupPrepareFile, createdAt time.Time, appVersion string) (backupManifestRunResult, error) {
	return a.runBackupPrepare(ctx, file, createdAt, appVersion,
		func(ctx context.Context, root string) (application.BackupDatabaseWork, error) {
			return platform.CreateBackupDatabaseWork(ctx, root)
		})
}

func (a *App) runBackupPrepare(ctx context.Context, file backupPrepareFile, createdAt time.Time, appVersion string,
	create func(context.Context, string) (application.BackupDatabaseWork, error),
) (backupManifestRunResult, error) {
	return a.useBackupArchive(ctx, createdAt, appVersion, create,
		func(ctx context.Context, copyPath string) ([]storage.ManagedCover, error) {
			return storage.DatabaseCopyCovers(ctx, copyPath, backup.MaxImages)
		}, func(ctx context.Context, manifest []byte, reader io.ReaderAt, root string, records []storage.ManagedCover) error {
			_, err := application.PrepareBackupArchiveOutput(ctx, file, func(dst io.Writer) error {
				return application.WriteBackupArchiveWithOperations(ctx, dst, manifest, reader, root, records, &a.coverOperations)
			})
			return err
		})
}
