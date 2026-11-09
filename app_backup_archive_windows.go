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

// writeBackupArchive borrows dst; no save target, Sync, Close or publication is
// performed. Any error (including cleanup/finish) invalidates all partial output.
func (a *App) writeBackupArchive(ctx context.Context, dst io.Writer, createdAt time.Time, appVersion string) (backupManifestRunResult, error) {
	return a.runBackupArchive(ctx, dst, createdAt, appVersion,
		func(ctx context.Context, root string) (application.BackupDatabaseWork, error) {
			return platform.CreateBackupDatabaseWork(ctx, root)
		}, func(ctx context.Context, copyPath string) ([]storage.ManagedCover, error) {
			return storage.DatabaseCopyCovers(ctx, copyPath, backup.MaxImages)
		})
}

func (a *App) runBackupArchive(ctx context.Context, dst io.Writer, createdAt time.Time, appVersion string,
	create func(context.Context, string) (application.BackupDatabaseWork, error),
	load func(context.Context, string) ([]storage.ManagedCover, error),
) (backupManifestRunResult, error) {
	return a.useBackupArchive(ctx, createdAt, appVersion, create, load,
		func(ctx context.Context, manifest []byte, reader io.ReaderAt, root string, records []storage.ManagedCover) error {
			return application.WriteBackupArchiveWithOperations(ctx, dst, manifest, reader, root, records, &a.coverOperations)
		})
}

// use is synchronous and runs before DB rechecks, cleanup and admission finish.
// It may prepare output, but must not publish or retain the borrowed DB reader.
func (a *App) useBackupArchive(ctx context.Context, createdAt time.Time, appVersion string,
	create func(context.Context, string) (application.BackupDatabaseWork, error),
	load func(context.Context, string) ([]storage.ManagedCover, error),
	use func(context.Context, []byte, io.ReaderAt, string, []storage.ManagedCover) error,
) (backupManifestRunResult, error) {
	return a.useBackupManifest(ctx, createdAt, appVersion, create, storage.DatabaseCopyMigrations,
		func(ctx context.Context, reader io.ReaderAt, copyPath string, manifest []byte) error {
			// SQL readers are closed before ZIP I/O. Preserve any SQL release mark
			// for runBackupDatabase's final admission decision.
			records, err := load(ctx, copyPath)
			if err != nil {
				return err
			}
			a.mu.RLock()
			root := a.dataDirectoryPath // Pinned at startup, never resolve again.
			a.mu.RUnlock()
			return use(ctx, manifest, reader, root, records)
		})
}
