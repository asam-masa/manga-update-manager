package application

import (
	"context"
	"errors"
	"io"

	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

// WriteBackupArchiveWithOperations retains image ownership through ZIP writing
// and parent release. The caller still owns normal admission, copy identity and
// parents, and must discard partial output on any error. No publication occurs.
func WriteBackupArchiveWithOperations(ctx context.Context, dst io.Writer, manifest []byte, db io.ReaderAt, directory string, records []storage.ManagedCover, operations *CoverOperations) error {
	return writeBackupArchiveWithOperations(ctx, dst, manifest, db, directory, records, operations, platform.HoldExistingCoverDirectory, ReadBackupArchiveCover)
}

func writeBackupArchiveWithOperations(ctx context.Context, dst io.Writer, manifest []byte, db io.ReaderAt, directory string, records []storage.ManagedCover, operations *CoverOperations,
	hold func(context.Context, string) (io.Closer, error),
	read func(context.Context, string, storage.ManagedCover, backup.Image) ([]byte, error),
) (err error) {
	defer func() {
		err = errors.Join(err, ctx.Err())
		if err != nil {
			err = errors.Join(ErrBackupArchive, err)
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	if operations == nil || dst == nil || db == nil {
		return ErrBackupArchive
	}
	observe := func(err error) {
		operations.ObserveError(err)
		if errors.Is(err, platform.ErrUnsafeCoverDirectory) {
			operations.Stop()
		}
	}
	op, err := operations.BeginRead()
	if err != nil {
		return err
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
		return err
	}
	defer unlock()
	err = func() (err error) {
		var parent io.Closer
		if err = stage(func() error { var e error; parent, e = hold(ctx, directory); return e }); err != nil {
			return err
		}
		defer func() {
			closeErr := parent.Close() // Mandatory even after Stop/cancellation.
			observe(closeErr)
			err = errors.Join(err, closeErr)
		}()
		output := backupArchiveOperationWriter(func(p []byte) (n int, err error) {
			err = stage(func() error { var e error; n, e = dst.Write(p); return e })
			return n, err
		})
		source := backupArchiveOperationReaderAt(func(p []byte, off int64) (n int, err error) {
			err = stage(func() error { var e error; n, e = db.ReadAt(p, off); return e })
			return n, err
		})
		return writeBackupArchive(ctx, output, manifest, source, directory, records,
			func(ctx context.Context, directory string, record storage.ManagedCover, expected backup.Image) (body []byte, err error) {
				err = stage(func() error { var e error; body, e = read(ctx, directory, record, expected); return e })
				return body, err
			})
	}()
	if err == nil {
		// Check Stop after the final admitted write and parent Close as well.
		err = stage(func() error { return nil })
	}
	return err
}

type backupArchiveOperationWriter func([]byte) (int, error)

func (w backupArchiveOperationWriter) Write(p []byte) (int, error) { return w(p) }

type backupArchiveOperationReaderAt func([]byte, int64) (int, error)

func (r backupArchiveOperationReaderAt) ReadAt(p []byte, off int64) (int, error) {
	return r(p, off)
}
