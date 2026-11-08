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
) (result backupImagesRunResult, err error) {
	var copyPath, directory string
	result.backupDatabaseRunResult, err = a.runBackupDatabase(ctx,
		func(ctx context.Context, _ io.ReaderAt, _ int64) error {
			// runBackupDatabase owns and validates the copy before calling use,
			// and retains it and normal admission through image release/cleanup.
			var err error
			result.Covers, err = inspect(ctx, copyPath, directory, &a.coverOperations)
			return err
		},
		func(ctx context.Context, root string) (application.BackupDatabaseWork, error) {
			work, err := create(ctx, root)
			if err == nil {
				copyPath, directory = work.Path(), root
			}
			return work, err
		})
	if err != nil {
		// Even a successful inventory cannot be used after a later copy check,
		// cleanup, cancellation or admission-finish failure.
		result.Covers = application.BackupCoverInventory{}
	}
	return result, err
}
