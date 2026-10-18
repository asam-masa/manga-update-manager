package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
	"github.com/asam-masa/manga-update-manager/internal/thumbnail"
)

type CoverRepository interface {
	WorkByID(context.Context, int64) (manga.Work, error)
	ReserveManagedCover(context.Context, string) error
	ConfirmManagedCover(context.Context, string, storage.ManagedCoverInfo) error
	SetWorkCover(context.Context, int64, string, time.Time) (manga.Work, string, error)
}

// Warnings are safe codes. Wails supplies user-facing messages, never raw errors.
type CoverImportResult struct {
	Cancelled bool
	Work      *manga.Work
	Warnings  []string
}

type CoverImporter struct {
	repository CoverRepository
	operations *CoverOperations
	directory  string
	choose     func(context.Context) (string, error)
	now        func() time.Time
	prepare    func(context.Context, string) (io.Closer, error)
	openSource func(context.Context, string) (io.ReadCloser, error)
	write      func(context.Context, string, string, []byte) (platform.CoverFileInfo, error)
}

func NewCoverImporter(repository CoverRepository, operations *CoverOperations, directory string, choose func(context.Context) (string, error), now func() time.Time) *CoverImporter {
	return &CoverImporter{repository: repository, operations: operations, directory: directory, choose: choose, now: now,
		prepare: platform.PrepareCoverDirectory, openSource: platform.OpenCoverSource, write: platform.WriteNewCover}
}

// Select owns the operation through all resource release. No file is deleted.
func (a *CoverImporter) Select(ctx context.Context, workID int64) (result CoverImportResult, err error) {
	result.Warnings = make([]string, 0)
	op, err := a.operations.BeginChange(workID, true)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, op.End()) }()
	stage := func(fn func() error) error {
		ticket, err := op.BeginStage()
		if err != nil {
			return err
		}
		err = ctx.Err()
		if err == nil {
			err = fn()
		}
		a.operations.ObserveError(err) // Before stage completion or any next stage.
		if errors.Is(err, platform.ErrUnsafeCoverDirectory) {
			a.operations.Stop()
		}
		ticket.End()
		return err
	}
	if err = stage(func() error { _, err := a.repository.WorkByID(ctx, workID); return err }); err != nil {
		return result, err
	}
	var path string
	if err = stage(func() error {
		var err error
		path, err = a.choose(ctx)
		op.SelectionDone()
		return err
	}); err != nil {
		return result, err
	}
	if path == "" {
		result.Cancelled = true
		return result, nil
	}
	var png []byte
	if err = stage(func() (err error) {
		source, err := a.openSource(ctx, path)
		if err != nil {
			return err
		}
		defer func() {
			if closeErr := source.Close(); closeErr != nil {
				err = errors.Join(err, platform.ErrCoverRelease, closeErr)
			}
		}()
		png, err = thumbnail.Generate(source)
		return err
	}); err != nil {
		return result, err
	}
	var guard io.Closer
	if err = stage(func() error { var err error; guard, err = a.prepare(ctx, a.directory); return err }); err != nil {
		return result, err
	}
	defer func() {
		closeErr := guard.Close()
		if closeErr != nil {
			closeErr = errors.Join(platform.ErrCoverRelease, closeErr)
			a.operations.ObserveError(closeErr)
			if result.Work != nil {
				result.Warnings = append(result.Warnings, "cover_release_unconfirmed")
			} else {
				err = errors.Join(err, closeErr)
			}
		}
	}()
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return result, err
	}
	id := hex.EncodeToString(random[:])
	if err = stage(func() error { return a.repository.ReserveManagedCover(ctx, id) }); err != nil {
		return result, err
	}
	var info platform.CoverFileInfo
	if err = stage(func() error { var err error; info, err = a.write(ctx, a.directory, id, png); return err }); err != nil {
		return result, err
	}
	if err = stage(func() error {
		return a.repository.ConfirmManagedCover(ctx, id, storage.ManagedCoverInfo{VolumeSerial: info.VolumeSerial, FileID: info.FileID, ByteSize: info.ByteSize, SHA256: info.SHA256})
	}); err != nil {
		return result, err
	}
	var work manga.Work
	var previous string
	err = stage(func() error {
		var err error
		work, previous, err = a.repository.SetWorkCover(ctx, workID, id, a.now())
		return err
	})
	if err != nil && !errors.Is(err, storage.ErrWorkCoverCommitted) {
		return result, err
	}
	result.Work = &work
	if err != nil {
		result.Warnings = append(result.Warnings, "database_stopped_after_commit")
		err = nil
	}
	if previous != "" {
		result.Warnings = append(result.Warnings, "cover_cleanup_deferred")
	}
	return result, nil
}
