package application

import (
	"context"
	"errors"
	"io"

	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/platform"
)

var ErrBackupDatabase = errors.New("backup database preparation or use failed")

type backupDatabaseWork interface {
	io.ReaderAt
	Path() string
	Size() (int64, error)
	VerifyIdentity() error
	Cleanup() (platform.BackupDatabaseCleanupResult, error)
}
type backupDatabaseCopier interface {
	CopyDatabase(context.Context, string) error
}

type BackupDatabaseResult struct {
	Copied         bool
	Validated      bool
	Consumed       bool
	Cleanup        platform.BackupDatabaseCleanupResult
	OperationError error
	CleanupError   error
}

// UseBackupDatabase consumes exclusive work ownership, including on cancellation.
// The caller holds application admission until all cleanup results are handled.
// use is synchronous and must not retain the reader or publish a completed backup.
// Stage observations do not mean publication or authorize resuming normal work.
func UseBackupDatabase(ctx context.Context, work backupDatabaseWork, copier backupDatabaseCopier, validate func(context.Context, string) error, use func(context.Context, io.ReaderAt, int64) error) (result BackupDatabaseResult, err error) {
	defer func() {
		result.OperationError = err
		result.Cleanup, result.CleanupError = work.Cleanup()
		err = errors.Join(err, result.CleanupError)
		if err != nil {
			err = errors.Join(ErrBackupDatabase, err)
		}
	}()
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if err = work.VerifyIdentity(); err != nil {
		return result, err
	}
	size, err := work.Size()
	if err != nil {
		return result, err
	}
	if size != 0 {
		return result, ErrBackupDatabase
	}
	path := work.Path()
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if err = copier.CopyDatabase(ctx, path); err != nil {
		return result, err
	}
	result.Copied = true
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if err = work.VerifyIdentity(); err != nil {
		return result, err
	}
	size, err = work.Size()
	if err != nil {
		return result, err
	}
	if size <= 0 || size > backup.MaxDatabaseBytes {
		return result, ErrBackupDatabase
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if err = validate(ctx, path); err != nil {
		return result, err
	}
	result.Validated = true
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if err = verifyBackupDatabaseSize(work, size); err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if err = use(ctx, work, size); err != nil {
		return result, err
	}
	result.Consumed = true
	if err = verifyBackupDatabaseSize(work, size); err != nil {
		return result, err
	}
	return result, ctx.Err()
}

func verifyBackupDatabaseSize(work backupDatabaseWork, want int64) error {
	if err := work.VerifyIdentity(); err != nil {
		return err
	}
	size, err := work.Size()
	if err != nil {
		return err
	}
	if size != want {
		return ErrBackupDatabase
	}
	return nil
}
