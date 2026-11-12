package main

import (
	"context"
	"errors"
	"io"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

type backupDatabaseRunResult struct {
	Database      application.BackupDatabaseResult
	Warnings      []CoverWarningDTO
	CreationError error
	FinishError   error
}

// useBackupDatabase holds admission through creation, synchronous reading, cleanup
// and finish. The reader must not escape use; publication belongs to a later step.
// No public Wails API exposes this internal path.
func (a *App) useBackupDatabase(ctx context.Context, use func(context.Context, io.ReaderAt, int64) error) (backupDatabaseRunResult, error) {
	return a.runBackupDatabase(ctx, use, func(ctx context.Context, directory string) (application.BackupDatabaseWork, error) {
		return platform.CreateBackupDatabaseWork(ctx, directory)
	})
}

func (a *App) runBackupDatabase(ctx context.Context, use func(context.Context, io.ReaderAt, int64) error, create func(context.Context, string) (application.BackupDatabaseWork, error)) (result backupDatabaseRunResult, err error) {
	return a.runBackupDatabaseFinalized(ctx, use, create, nil)
}

// finalize runs after all DB rechecks/cleanup, before releasing admission. It
// must preserve the incoming error and report release of its own output handles.
type backupDatabaseFinalizer func(error) (bool, error)

func (a *App) runBackupDatabaseFinalized(ctx context.Context, use func(context.Context, io.ReaderAt, int64) error,
	create func(context.Context, string) (application.BackupDatabaseWork, error), finalize backupDatabaseFinalizer,
) (result backupDatabaseRunResult, err error) {
	var op *application.BackupOperation
	op, result.Warnings, err = a.beginBackup(ctx)
	if err != nil {
		return result, err
	}
	// No owned work exists yet. Once creation starts, require explicit release
	// confirmation even if it returns no owner. Never infer it from cancellation.
	released := true
	defer func() {
		if finalize != nil {
			if !released {
				err = errors.Join(err, errBackupReleaseUnverified)
			}
			outputReleased, finalErr := finalize(errors.Join(err, ctx.Err()))
			released = released && outputReleased
			err = finalErr
		}
		result.FinishError = a.finishBackup(op, released)
		// Cleanup does not rewrite the completed DB stage's outcome. Still pass
		// cancellation observed during cleanup to this outer caller.
		err = errors.Join(err, result.FinishError, ctx.Err())
	}()
	a.mu.RLock()
	directory, store := a.dataDirectoryPath, a.store
	a.mu.RUnlock()
	if directory == "" {
		return result, unavailableError()
	}
	released = false
	work, err := create(ctx, directory)
	if err != nil {
		result.CreationError = err
		var detail *platform.BackupDatabaseWorkError
		if errors.As(err, &detail) {
			released = detail.ReleaseConfirmed()
		}
		return result, err
	}
	result.Database, err = application.UseBackupDatabase(ctx, work, store, storage.ValidateDatabaseCopy, use)
	// The work's Windows handles do not establish release of SQL readers opened
	// while validating or consuming the copy. Keep ambiguous SQL cleanup stopped.
	released = result.Database.Cleanup.ReleaseConfirmed() && !errors.Is(err, storage.ErrDatabaseCopyRelease)
	return result, err
}
