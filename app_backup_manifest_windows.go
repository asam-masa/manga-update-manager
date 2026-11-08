package main

import (
	"context"
	"io"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

type backupManifestRunResult struct {
	backupImagesRunResult
	Manifest []byte
}

// prepareBackupManifest is internal preparation, not a saved backup. On return
// the owned DB work has been cleaned up; no reader escapes for later ZIP use.
func (a *App) prepareBackupManifest(ctx context.Context, createdAt time.Time, appVersion string) (backupManifestRunResult, error) {
	return a.runBackupManifest(ctx, createdAt, appVersion,
		func(ctx context.Context, root string) (application.BackupDatabaseWork, error) {
			return platform.CreateBackupDatabaseWork(ctx, root)
		}, storage.DatabaseCopyMigrations)
}

func (a *App) runBackupManifest(ctx context.Context, createdAt time.Time, appVersion string,
	create func(context.Context, string) (application.BackupDatabaseWork, error),
	load func(context.Context, string) ([]string, error),
) (result backupManifestRunResult, err error) {
	result.backupImagesRunResult, err = a.useBackupImages(ctx, create, application.InspectBackupCopyCovers,
		func(ctx context.Context, reader io.ReaderAt, size int64, copyPath string, covers application.BackupCoverInventory) error {
			versions, err := load(ctx, copyPath)
			if err != nil {
				return err // Preserve SQL release marks for the outer finish decision.
			}
			result.Manifest, err = application.BuildBackupManifest(ctx, reader, size, application.BackupManifestMetadata{
				CreatedAt: createdAt, AppVersion: appVersion, Migrations: versions, Covers: covers,
			})
			return err
		})
	if err != nil {
		// Successful encoding is insufficient if later DB checks/cleanup/finish
		// fail. The image path likewise clears its inventory on any outer error.
		result.Manifest = nil
	}
	return result, err
}
