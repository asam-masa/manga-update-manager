package main

import (
	"context"
	"io"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/platform"
)

type backupImagesRunResult struct {
	backupDatabaseRunResult
	Covers application.BackupCoverInventory
}

// inspectBackupImages prepares metadata only. No public Wails API exposes it.
// The copy reader and inventory are not a saved or publishable backup.
func (a *App) inspectBackupImages(ctx context.Context) (backupImagesRunResult, error) {
	return a.runBackupImages(ctx, func(ctx context.Context, directory string) (application.BackupDatabaseWork, error) {
		return platform.CreateBackupDatabaseWork(ctx, directory)
	}, application.InspectBackupCopyCovers)
}

func (a *App) runBackupImages(ctx context.Context,
	create func(context.Context, string) (application.BackupDatabaseWork, error),
	inspect func(context.Context, string, string, *application.CoverOperations) (application.BackupCoverInventory, error),
) (backupImagesRunResult, error) {
	return a.useBackupImages(ctx, create, inspect, func(context.Context, io.ReaderAt, int64, string, application.BackupCoverInventory) error { return nil }, nil)
}

// use must be synchronous, borrow only, and never publish a completed backup.
// Admission and DB work ownership outlive use, including all cleanup attempts.
func (a *App) useBackupImages(ctx context.Context,
	create func(context.Context, string) (application.BackupDatabaseWork, error),
	inspect func(context.Context, string, string, *application.CoverOperations) (application.BackupCoverInventory, error),
	use func(context.Context, io.ReaderAt, int64, string, application.BackupCoverInventory) error,
	finalize backupDatabaseFinalizer,
) (result backupImagesRunResult, err error) {
	var copyPath, directory string
	result.backupDatabaseRunResult, err = a.runBackupDatabaseFinalized(ctx,
		func(ctx context.Context, reader io.ReaderAt, size int64) error {
			// runBackupDatabase owns and validates the copy before calling use,
			// and retains it and normal admission through image release/cleanup.
			var err error
			result.Covers, err = inspect(ctx, copyPath, directory, &a.coverOperations)
			if err != nil {
				return err
			}
			return use(ctx, reader, size, copyPath, result.Covers)
		},
		func(ctx context.Context, root string) (application.BackupDatabaseWork, error) {
			work, err := create(ctx, root)
			if err == nil {
				copyPath, directory = work.Path(), root
			}
			return work, err
		}, finalize)
	if err != nil {
		// Even a successful inventory cannot be used after a later copy check,
		// cleanup, cancellation or admission-finish failure.
		result.Covers = application.BackupCoverInventory{}
	}
	return result, err
}
