package application

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/asam-masa/manga-update-manager/internal/backup"
)

var ErrBackupPrepare = errors.New("backup archive preparation failed")

// The consumer only needs operations on a retained temporary file. The caller
// exclusively owns it during preparation and remains responsible for Close,
// owned cleanup, publication and propagating their outcomes afterwards.
type backupArchiveFile interface {
	io.Writer
	io.ReaderAt
	Sync() error
	Size() (int64, error)
	VerifyIdentity() error
}

// PrepareBackupArchive writes, syncs and verifies an empty retained file.
// Success is not publication, durability after power loss, SQLite compatibility
// or image validation. Failure returns no partial manifest and leaves the
// temporary file under caller ownership; it never closes or deletes it.
// Cancellation is cooperative and cannot interrupt a blocked OS operation.
func PrepareBackupArchive(ctx context.Context, file backupArchiveFile, manifest []byte, open func(string) (io.ReadCloser, error)) (backup.Manifest, error) {
	return PrepareBackupArchiveOutput(ctx, file, func(dst io.Writer) error {
		return backup.WriteArchive(ctx, dst, manifest, open)
	})
}

// PrepareBackupArchiveOutput applies the same retained-file checks to a
// synchronous ZIP producer. write borrows only Writer and must finish all writes
// before returning. The caller retains exclusive ownership and must propagate
// later source checks/cleanup failures before publishing. This never closes,
// deletes or publishes file; success only establishes archive preparation.
func PrepareBackupArchiveOutput(ctx context.Context, file backupArchiveFile, write func(io.Writer) error) (backup.Manifest, error) {
	m, err := prepareBackupArchiveOutput(ctx, file, write)
	if err != nil {
		return backup.Manifest{}, fmt.Errorf("%w: %w", ErrBackupPrepare, err)
	}
	return m, nil
}

func prepareBackupArchiveOutput(ctx context.Context, file backupArchiveFile, write func(io.Writer) error) (backup.Manifest, error) {
	if err := ctx.Err(); err != nil {
		return backup.Manifest{}, err
	}
	if err := file.VerifyIdentity(); err != nil {
		return backup.Manifest{}, err
	}
	size, err := file.Size()
	if err != nil {
		return backup.Manifest{}, err
	}
	if size != 0 {
		return backup.Manifest{}, errors.New("backup temporary file is not empty")
	}
	if err := write(file); err != nil {
		return backup.Manifest{}, err
	}
	if err := ctx.Err(); err != nil {
		return backup.Manifest{}, err
	}
	if err := file.Sync(); err != nil {
		return backup.Manifest{}, err
	}
	if err := ctx.Err(); err != nil {
		return backup.Manifest{}, err
	}
	size, err = file.Size()
	if err != nil {
		return backup.Manifest{}, err
	}
	m, err := backup.ValidateArchive(backupContextReaderAt{ctx: ctx, file: file}, size)
	if err != nil {
		return backup.Manifest{}, err
	}
	if err := file.VerifyIdentity(); err != nil {
		return backup.Manifest{}, err
	}
	if err := ctx.Err(); err != nil {
		return backup.Manifest{}, err
	}
	return m, nil
}

type backupContextReaderAt struct {
	ctx  context.Context
	file io.ReaderAt
}

func (r backupContextReaderAt) ReadAt(p []byte, offset int64) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.file.ReadAt(p, offset)
}
