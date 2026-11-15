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
	// Final resource/source observation only; publication and fresh start checks
	// remain separate. False is the safe default when no owner confirmed it.
	RetrySafetyConfirmed bool
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
	return a.runBackupDatabaseFinalized(ctx, use, create, nil, nil)
}

// finalize runs after all DB rechecks/cleanup, before releasing admission. It
// must preserve the incoming error and report release of its own output handles.
type backupDatabaseFinalizer func(error) (bool, error)

// The selected runner either owns admission or borrows the caller's reservation.
// Choose it at the save entry point; never infer ownership from context values.
type backupDatabaseRunner func(context.Context, func(context.Context, io.ReaderAt, int64) error,
	func(context.Context, string) (application.BackupDatabaseWork, error), backupDatabaseFinalizer, application.BackupProgress,
) (backupDatabaseRunResult, error)

func (a *App) runBackupDatabaseFinalized(ctx context.Context, use func(context.Context, io.ReaderAt, int64) error,
	create func(context.Context, string) (application.BackupDatabaseWork, error), finalize backupDatabaseFinalizer, report application.BackupProgress,
) (result backupDatabaseRunResult, err error) {
	var op *application.BackupOperation
	report.Notify(backupStageWaiting)
	op, result.Warnings, err = a.beginBackup(ctx)
	if err != nil {
		return result, err
	}
	// Only this outer owner may finish admission. Fail closed if the borrowed
	// worker does not return its release observation (for example on panic).
	released := false
	defer func() {
		result.FinishError = a.finishBackup(op, released)
		result.RetrySafetyConfirmed = result.FinishError == nil
		err = errors.Join(err, result.FinishError, ctx.Err())
	}()
	warnings := result.Warnings
	result, released, err = a.runReservedBackupDatabase(ctx, use, create, finalize, report)
	result.Warnings = warnings
	return result, err
}

// runReservedBackupDatabase borrows already prepared, paused admission from its
// caller. It never reserves, ends or resumes admission. The caller must retain
// the reservation through this return and finish using the release observation,
// including DB/SQL and output-finalizer resources, not merely err == nil.
// No reader may escape use. This is not a public Wails entry point.
func (a *App) runReservedBackupDatabase(ctx context.Context, use func(context.Context, io.ReaderAt, int64) error,
	create func(context.Context, string) (application.BackupDatabaseWork, error), finalize backupDatabaseFinalizer, report application.BackupProgress,
) (result backupDatabaseRunResult, released bool, err error) {
	// No owned work exists yet. Once creation starts, require explicit release
	// confirmation even if it returns no owner. Never infer it from cancellation.
	released = true
	defer func() {
		if finalize != nil {
			if !released {
				err = errors.Join(err, errBackupReleaseUnverified)
			}
			outputReleased, finalErr := finalize(errors.Join(err, ctx.Err()))
			released = released && outputReleased
			err = finalErr
		}
		// Cleanup does not rewrite the completed DB stage's outcome. Still pass
		// cancellation observed during cleanup to this outer caller.
		err = errors.Join(err, ctx.Err())
	}()
	a.mu.RLock()
	directory, store := a.dataDirectoryPath, a.store
	a.mu.RUnlock()
	if directory == "" {
		return result, released, unavailableError()
	}
	released = false
	report.Notify(backupStagePreparing)
	work, err := create(ctx, directory)
	if err != nil {
		result.CreationError = err
		var detail *platform.BackupDatabaseWorkError
		if errors.As(err, &detail) {
			released = detail.ReleaseConfirmed()
		}
		return result, released, err
	}
	result.Database, err = application.UseBackupDatabaseWithProgress(ctx, work, store, storage.ValidateDatabaseCopy, use, report)
	// The work's Windows handles do not establish release of SQL readers opened
	// while validating or consuming the copy. Keep ambiguous SQL cleanup stopped.
	released = result.Database.Cleanup.ReleaseConfirmed() && !errors.Is(err, storage.ErrDatabaseCopyRelease)
	return result, released, err
}
