package main

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

var errBackupPublicationDenied = errors.New("backup execution did not allow publication")

// startBackupSave accepts only a Go-approved destination. The public picker/API
// remains separate; this worker uses the application's lifetime and reservation.
func (a *App) startBackupSave(destination string, createdAt time.Time, version string) (backupRun, error) {
	return a.startBackupRun(destination, a.backupSaveWorker(destination, createdAt, version))
}

func (a *App) backupSaveWorker(destination string, createdAt time.Time, version string) func(context.Context, application.BackupExecutionID) (backupSaveRunResult, bool, error) {
	return func(ctx context.Context, id application.BackupExecutionID) (backupSaveRunResult, bool, error) {
		return a.runReservedBackupSave(ctx, id, destination, createdAt, version,
			func(ctx context.Context, root string) (application.BackupDatabaseWork, error) {
				return platform.CreateBackupDatabaseWork(ctx, root)
			}, func(ctx context.Context, target string) (backupSaveFile, error) {
				return platform.CreateBackupTemp(ctx, target)
			}, storage.DatabaseCopyCoverWorks)
	}
}

// Caller retains prepared admission until all releases and the outcome return.
// The same pipeline serves synchronous and managed saves; only the DB owner and
// the publication permission differ. No context values or App-wide mode switch.
func (a *App) runReservedBackupSave(ctx context.Context, id application.BackupExecutionID, destination string, createdAt time.Time, version string,
	createWork func(context.Context, string) (application.BackupDatabaseWork, error),
	createOutput func(context.Context, string) (backupSaveFile, error),
	loadDetails func(context.Context, string, []string) ([]storage.CopyCoverWork, error),
) (result backupSaveRunResult, released bool, err error) {
	database := func(ctx context.Context, use func(context.Context, io.ReaderAt, int64) error,
		create func(context.Context, string) (application.BackupDatabaseWork, error), finalize backupDatabaseFinalizer, report application.BackupProgress,
	) (backupDatabaseRunResult, error) {
		var r backupDatabaseRunResult
		var err error
		r, released, err = a.runReservedBackupDatabase(ctx, use, create, finalize, report)
		return r, err
	}
	result, err = a.runBackupSaveWithDatabase(ctx, destination, createdAt, version, createWork,
		func(ctx context.Context, target string) (backupSaveFile, error) {
			file, err := createOutput(ctx, target)
			if err != nil {
				return nil, err
			}
			return backupExecutionFile{backupSaveFile: file, owner: &a.backupRuns, id: id}, nil
		}, loadDetails, func(stage application.BackupStage) { a.backupRuns.reportStage(id, stage) }, database)
	return result, released, err
}

// Gate the real publication call, after preparation and DB cleanup. Cancel and
// BeginPublication share the execution lock; no I/O occurs under that lock.
type backupExecutionFile struct {
	backupSaveFile
	owner *backupRunState
	id    application.BackupExecutionID
}

func (f backupExecutionFile) PublishNoReplace(ctx context.Context) (platform.BackupPublication, error) {
	if !f.owner.beginPublication(f.id) {
		return platform.BackupPublication{State: platform.BackupPublishNotAttempted, TemporaryPath: f.Path()}, errors.Join(errBackupPublicationDenied, ctx.Err())
	}
	return f.backupSaveFile.PublishNoReplace(ctx)
}
