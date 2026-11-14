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
	Omissions              backupOmissionObservation
	OutputCreationError    error
	OutputReleaseConfirmed bool
}

// saveBackupArchive is internal; callers must provide an approved destination.
// Publication observations survive postprocessing/finish errors and cancellation.
func (a *App) saveBackupArchive(ctx context.Context, destination string, createdAt time.Time, appVersion string) (backupSaveRunResult, error) {
	return a.saveBackupArchiveWithProgress(ctx, destination, createdAt, appVersion, nil)
}

// report is bound to one run by the caller; no global current run is inferred.
// Connecting this save worker to execution/publication control remains separate.
func (a *App) saveBackupArchiveWithProgress(ctx context.Context, destination string, createdAt time.Time, appVersion string, report application.BackupProgress) (backupSaveRunResult, error) {
	return a.runBackupSaveObserved(ctx, destination, createdAt, appVersion,
		func(ctx context.Context, root string) (application.BackupDatabaseWork, error) {
			return platform.CreateBackupDatabaseWork(ctx, root)
		}, func(ctx context.Context, target string) (backupSaveFile, error) {
			return platform.CreateBackupTemp(ctx, target)
		}, storage.DatabaseCopyCoverWorks, report)
}

func (a *App) runBackupSave(ctx context.Context, destination string, createdAt time.Time, appVersion string,
	createWork func(context.Context, string) (application.BackupDatabaseWork, error),
	createOutput func(context.Context, string) (backupSaveFile, error),
) (result backupSaveRunResult, err error) {
	return a.runBackupSaveObserved(ctx, destination, createdAt, appVersion, createWork, createOutput, storage.DatabaseCopyCoverWorks, nil)
}

func (a *App) runBackupSaveObserved(ctx context.Context, destination string, createdAt time.Time, appVersion string,
	createWork func(context.Context, string) (application.BackupDatabaseWork, error),
	createOutput func(context.Context, string) (backupSaveFile, error),
	loadDetails func(context.Context, string, []string) ([]storage.CopyCoverWork, error),
	report application.BackupProgress,
) (result backupSaveRunResult, err error) {
	var file backupSaveFile
	result.OutputReleaseConfirmed = true // No output handles acquired yet.
	result.backupManifestRunResult, err = a.useBackupArchiveObserved(ctx, createdAt, appVersion, createWork,
		func(ctx context.Context, copyPath string) ([]storage.ManagedCover, error) {
			return storage.DatabaseCopyCovers(ctx, copyPath, backup.MaxImages)
		}, func(ctx context.Context, manifest []byte, reader io.ReaderAt, root string, records []storage.ManagedCover) error {
			report.Notify(backupStageZIP)
			created, err := createOutput(ctx, destination)
			if err != nil {
				result.OutputCreationError = err
				return err
			}
			file = backupProgressFile{backupSaveFile: created, report: report} // Creation failure never returns an owner.
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
		}, func(ctx context.Context, copyPath string, manifest []byte) error {
			var detailErr error
			result.Omissions, detailErr = a.observeBackupOmissions(ctx, copyPath, manifest, loadDetails)
			return detailErr
		}, report)
	return result, err
}

// Decorate the actual output entry points; never guess publication/cleanup from
// the preparation result. All ownership, return values and errors pass through.
type backupProgressFile struct {
	backupSaveFile
	report application.BackupProgress
}

func (f backupProgressFile) PublishNoReplace(ctx context.Context) (platform.BackupPublication, error) {
	f.report.Notify(backupStagePublishing)
	return f.backupSaveFile.PublishNoReplace(ctx)
}

func (f backupProgressFile) Discard() (platform.BackupDiscardResult, error) {
	f.report.Notify(backupStageCleanup)
	return f.backupSaveFile.Discard()
}

func (f backupProgressFile) Close() error {
	f.report.Notify(backupStageCleanup)
	return f.backupSaveFile.Close()
}
