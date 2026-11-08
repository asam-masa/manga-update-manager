package application

import (
	"context"
	"errors"
	"io"

	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

// InspectBackupCopyCovers connects copy metadata and image inspection. The caller
// retains outer admission, validated copy identity and its parents through return.
// SQL release marks must reach the outer finish decision unchanged. This function
// neither owns the DB work nor publishes a backup or authorizes resuming work.
func InspectBackupCopyCovers(ctx context.Context, copyPath, directory string, operations *CoverOperations) (BackupCoverInventory, error) {
	return inspectBackupCopyCovers(ctx, copyPath, directory, operations, storage.DatabaseCopyCovers, platform.HoldExistingCoverDirectory, InspectBackupCover)
}

func inspectBackupCopyCovers(ctx context.Context, copyPath, directory string, operations *CoverOperations,
	load func(context.Context, string, int) ([]storage.ManagedCover, error),
	hold func(context.Context, string) (io.Closer, error),
	inspect func(context.Context, string, storage.ManagedCover) (BackupCoverInspection, error),
) (result BackupCoverInventory, err error) {
	defer func() {
		err = errors.Join(err, ctx.Err())
		if err != nil {
			result = BackupCoverInventory{}
			err = errors.Join(ErrBackupCovers, err)
		}
	}()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if operations == nil {
		return result, ErrBackupCovers
	}
	observe := func(err error) {
		operations.ObserveError(err)
		if errors.Is(err, platform.ErrUnsafeCoverDirectory) {
			operations.Stop()
		}
	}
	op, err := operations.BeginRead()
	if err != nil {
		return result, err
	}
	defer func() { endErr := op.End(); observe(endErr); err = errors.Join(err, endErr) }()
	stage := func(fn func() error) error {
		ticket, err := op.BeginReadStage()
		if err != nil {
			return err
		}
		defer ticket.End()
		err = ctx.Err()
		if err == nil {
			err = fn()
		}
		observe(err)
		return err
	}
	var unlock func()
	if err = stage(func() error { var e error; unlock, e = operations.LockReferences(ctx); return e }); err != nil {
		return result, err
	}
	defer unlock()
	var records []storage.ManagedCover
	if err = stage(func() error { var e error; records, e = load(ctx, copyPath, backup.MaxImages); return e }); err != nil {
		return result, err
	}
	result, err = inspectBackupCovers(ctx, directory, records,
		func(ctx context.Context, directory string) (io.Closer, error) {
			var resource io.Closer
			if err := stage(func() error { var e error; resource, e = hold(ctx, directory); return e }); err != nil {
				return nil, err
			}
			return observedBackupCoverCloser{Closer: resource, observe: observe}, nil
		},
		func(ctx context.Context, directory string, record storage.ManagedCover) (out BackupCoverInspection, err error) {
			err = stage(func() error { var e error; out, e = inspect(ctx, directory, record); return e })
			return out, err
		})
	if err == nil {
		// Even an empty inventory or a Stop during the final admitted stage must
		// be checked before reporting success. Owned cleanup has already run.
		err = stage(func() error { return nil })
	}
	return result, err
}

type observedBackupCoverCloser struct {
	io.Closer
	observe func(error)
}

func (c observedBackupCoverCloser) Close() error {
	err := c.Closer.Close() // Release remains mandatory after Stop; no new stage.
	c.observe(err)
	return err
}
