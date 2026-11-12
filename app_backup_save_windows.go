package main

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

type backupSaveFile interface {
	backupPrepareFile
	Path() string
	PublishNoReplace(context.Context) (platform.BackupPublication, error)
	Discard() (platform.BackupDiscardResult, error)
	Close() error
}

type backupSaveRunResult struct {
	backupManifestRunResult
	Save                   application.BackupSaveResult
	OutputCreationError    error
	OutputReleaseConfirmed bool
}

// saveBackupArchive is internal; callers must provide an approved destination.
// Publication observations survive postprocessing/finish errors and cancellation.
func (a *App) saveBackupArchive(ctx context.Context, destination string, createdAt time.Time, appVersion string) (backupSaveRunResult, error) {
	return a.runBackupSave(ctx, destination, createdAt, appVersion,
		func(ctx context.Context, root string) (application.BackupDatabaseWork, error) {
			return platform.CreateBackupDatabaseWork(ctx, root)
		}, func(ctx context.Context, target string) (backupSaveFile, error) {
			return platform.CreateBackupTemp(ctx, target)
		})
}

func (a *App) runBackupSave(ctx context.Context, destination string, createdAt time.Time, appVersion string,
	createWork func(context.Context, string) (application.BackupDatabaseWork, error),
	createOutput func(context.Context, string) (backupSaveFile, error),
) (result backupSaveRunResult, err error) {
	var file backupSaveFile
	result.OutputReleaseConfirmed = true // No output handles acquired yet.
	result.backupManifestRunResult, err = a.useBackupArchive(ctx, createdAt, appVersion, createWork,
		func(ctx context.Context, copyPath string) ([]storage.ManagedCover, error) {
			return storage.DatabaseCopyCovers(ctx, copyPath, backup.MaxImages)
		}, func(ctx context.Context, manifest []byte, reader io.ReaderAt, root string, records []storage.ManagedCover) error {
			created, err := createOutput(ctx, destination)
			if err != nil {
				result.OutputCreationError = err
				return err
			}
			file = created // Failure returns no owner, even a typed nil interface.
			_, err = application.PrepareBackupArchiveOutput(ctx, file, func(dst io.Writer) error {
				return application.WriteBackupArchiveWithOperations(ctx, dst, manifest, reader, root, records, &a.coverOperations)
			})
			return err
		}, func(preparationErr error) (bool, error) {
			// The copy's rechecks and Cleanup have finished, but admission is still
			// reserved. Never publish after a source/cleanup/stop/cancellation error.
			err := errors.Join(preparationErr, a.checkBackupState(ctx))
			if file != nil {
				result.Save, err = application.FinishBackupArchive(ctx, file, err)
			}
			result.OutputReleaseConfirmed = !errors.Is(err, platform.ErrBackupTempRelease) && !errors.Is(err, platform.ErrBackupDirectoryRelease)
			return result.OutputReleaseConfirmed, err
		})
	return result, err
}
