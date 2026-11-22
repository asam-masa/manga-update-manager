package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"

	"github.com/asam-masa/manga-update-manager/internal/backup"
)

var ErrRestoreReadback = errors.New("restore preparation readback failed")

type restoreReadbackFile interface {
	io.ReaderAt
	Sync() error
	Size() (int64, error)
}

// VerifyRestorePreparationFile borrows one created/retained preparation file.
// The caller exclusively owns the file and parents until this call finishes;
// no concurrent writes, Close or cleanup are allowed. Expectations come from
// the validated manifest (including its per-type image/DB limits). The largest
// supported component bound is enforced here too. No path or raw handle is used.
// Success confirms sync/readback only, not identity, DB/PNG semantics, admission,
// publication or power-loss durability. This never closes or deletes the file.
// Cancellation is cooperative around operations, not a forced OS interruption.
func VerifyRestorePreparationFile(ctx context.Context, file restoreReadbackFile, expectedSize int64, expectedSHA256 [sha256.Size]byte) (err error) {
	defer func() {
		if err = errors.Join(err, ctx.Err()); err != nil {
			err = errors.Join(ErrRestoreReadback, err)
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	if file == nil || expectedSize <= 0 || expectedSize > backup.MaxDatabaseBytes {
		return backup.ErrManifest
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	size, err := file.Size()
	if err != nil {
		return err
	}
	if size != expectedSize {
		return errors.New("restore preparation size mismatch")
	}
	h := sha256.New()
	reader := io.NewSectionReader(restoreReadbackReaderAt{ctx, file}, 0, expectedSize+1)
	n, err := io.CopyBuffer(h, reader, make([]byte, 32<<10))
	if err != nil {
		return err
	}
	if n != expectedSize || !bytes.Equal(h.Sum(nil), expectedSHA256[:]) {
		return errors.New("restore preparation content mismatch")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	size, err = file.Size()
	if err != nil {
		return err
	}
	if size != expectedSize {
		return errors.New("restore preparation size changed during readback")
	}
	return nil
}

type restoreReadbackReaderAt struct {
	ctx  context.Context
	file io.ReaderAt
}

func (r restoreReadbackReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.file.ReadAt(p, off)
	if err == nil && n != len(p) {
		err = io.ErrUnexpectedEOF
	}
	if cancelled := r.ctx.Err(); cancelled != nil {
		err = errors.Join(err, cancelled)
	}
	return n, err
}
