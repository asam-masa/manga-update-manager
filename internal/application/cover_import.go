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
	ListManagedCovers(context.Context) ([]storage.ManagedCover, error)
	ReserveManagedCover(context.Context, string) error
	ConfirmManagedCover(context.Context, string, storage.ManagedCoverInfo) error
	SetWorkCover(context.Context, int64, string, time.Time) (manga.Work, string, error)
	RemoveWorkCover(context.Context, int64, time.Time) (manga.Work, string, error)
	UnusedManagedCover(context.Context, string) (storage.ManagedCover, bool, error)
	ForgetUnusedManagedCover(context.Context, storage.ManagedCover) error
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
	delete     func(context.Context, string, string, platform.CoverFileInfo) error
}

func NewCoverImporter(repository CoverRepository, operations *CoverOperations, directory string, choose func(context.Context) (string, error), now func() time.Time) *CoverImporter {
	return &CoverImporter{repository: repository, operations: operations, directory: directory, choose: choose, now: now,
		prepare: platform.PrepareCoverDirectory, openSource: platform.OpenCoverSource, write: platform.WriteNewCover, delete: platform.DeleteVerifiedCover}
}

// Select owns the operation through cleanup and all resource release.
func (a *CoverImporter) Select(ctx context.Context, workID int64) (result CoverImportResult, err error) {
	return a.SelectWithSelectionDone(ctx, workID, nil)
}

// SelectWithSelectionDone reports chooser completion after releasing the shared
// selection slot. The callback belongs to this request, not to the importer.
// It must return promptly rather than wait for a later import to complete.
func (a *CoverImporter) SelectWithSelectionDone(ctx context.Context, workID int64, selectionDone func()) (CoverImportResult, error) {
	return a.SelectWithProgress(ctx, workID, selectionDone, nil)
}

// SelectWithProgress adds request-local stage observations to chooser completion.
func (a *CoverImporter) SelectWithProgress(ctx context.Context, workID int64, selectionDone func(), progress CoverProgress) (CoverImportResult, error) {
	return a.change(ctx, workID, true, selectionDone, progress)
}

// Remove shares the commit, cleanup and release boundaries with Select, but
// never opens a chooser or reads/generates a source image.
func (a *CoverImporter) Remove(ctx context.Context, workID int64) (CoverImportResult, error) {
	return a.RemoveWithProgress(ctx, workID, nil)
}

func (a *CoverImporter) RemoveWithProgress(ctx context.Context, workID int64, progress CoverProgress) (CoverImportResult, error) {
	return a.change(ctx, workID, false, nil, progress)
}

func (a *CoverImporter) change(ctx context.Context, workID int64, selecting bool, selectionDone func(), progress CoverProgress) (result CoverImportResult, err error) {
	result.Warnings = make([]string, 0)
	op, err := a.operations.BeginChange(workID, selecting)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, op.End()) }()
	var unlockReferences func()
	// Registered before the guard defer: notify its release outcome before
	// allowing another operation to enter the reference/cleanup section.
	defer func() {
		if unlockReferences != nil {
			unlockReferences()
		}
	}()
	stage := func(code CoverProgressStage, fn func() error) error {
		return a.progressStage(ctx, op, progress, code, fn)
	}
	if err = stage(CoverProgressChecking, func() error { _, err := a.repository.WorkByID(ctx, workID); return err }); err != nil {
		return result, err
	}
	var path string
	var png []byte
	if selecting {
		if err = stage(CoverProgressChoosing, func() error {
			var err error
			path, err = a.choose(ctx)
			op.SelectionDone()
			if selectionDone != nil {
				selectionDone()
			}
			return err
		}); err != nil {
			return result, err
		}
		if path == "" {
			result.Cancelled = true
			return result, nil
		}
		if err = stage(CoverProgressGenerating, func() (err error) {
			source, err := a.openSource(ctx, path)
			if err != nil {
				return err
			}
			defer func() {
				progress.report(CoverProgressReleasing)
				if closeErr := source.Close(); closeErr != nil {
					err = errors.Join(err, platform.ErrCoverRelease, closeErr)
				}
			}()
			png, err = thumbnail.Generate(source)
			return err
		}); err != nil {
			return result, err
		}
	}
	var guard io.Closer
	if err = stage(CoverProgressPreparing, func() error { var err error; guard, err = a.prepare(ctx, a.directory); return err }); err != nil {
		return result, err
	}
	defer func() {
		progress.report(CoverProgressReleasing)
		closeErr := guard.Close()
		if closeErr != nil {
			closeErr = errors.Join(platform.ErrCoverRelease, closeErr)
			a.operations.ObserveError(closeErr)
			if result.Work != nil {
				result.warn("cover_release_unconfirmed")
			} else {
				err = errors.Join(err, closeErr)
			}
		}
	}()
	var id string
	var savedInfo storage.ManagedCoverInfo
	if selecting {
		var random [16]byte
		if _, err = rand.Read(random[:]); err != nil {
			return result, err
		}
		id = hex.EncodeToString(random[:])
		if err = stage(CoverProgressSaving, func() error { return a.repository.ReserveManagedCover(ctx, id) }); err != nil {
			return result, err
		}
		var info platform.CoverFileInfo
		if err = stage(CoverProgressSaving, func() error { var err error; info, err = a.write(ctx, a.directory, id, png); return err }); err != nil {
			return result, err
		}
		savedInfo = storage.ManagedCoverInfo{VolumeSerial: info.VolumeSerial, FileID: info.FileID, ByteSize: info.ByteSize, SHA256: info.SHA256}
	}
	if err = stage(CoverProgressWaiting, func() error {
		var err error
		unlockReferences, err = a.operations.LockReferences(ctx)
		return err
	}); err != nil {
		return result, err
	}
	if selecting {
		if err = stage(CoverProgressUpdating, func() error {
			// The saved output is re-read by WriteNewCover before this boundary.
			// Keep its identity until confirmation without exposing it to Wails.
			return a.repository.ConfirmManagedCover(ctx, id, savedInfo)
		}); err != nil {
			return result, err
		}
	}
	var work manga.Work
	var previous string
	err = stage(CoverProgressUpdating, func() error {
		var err error
		if selecting {
			work, previous, err = a.repository.SetWorkCover(ctx, workID, id, a.now())
		} else {
			work, previous, err = a.repository.RemoveWorkCover(ctx, workID, a.now())
		}
		return err
	})
	if err != nil && !errors.Is(err, storage.ErrWorkCoverCommitted) {
		return result, err
	}
	result.Work = &work
	if err != nil {
		result.warn("database_stopped_after_commit")
		if previous != "" {
			result.warn("cover_cleanup_failed")
		}
		err = nil
		return result, nil // Stopped after commit: never begin cleanup.
	}
	if previous != "" {
		cleanupStage := func(fn func() error) error { return stage(CoverProgressCleaning, fn) }
		if cleanupErr := a.cleanupPrevious(ctx, previous, cleanupStage); cleanupErr != nil {
			result.warn("cover_cleanup_failed")
			if errors.Is(cleanupErr, storage.ErrDBStopped) {
				result.warn("database_stopped_after_commit")
			}
			if errors.Is(cleanupErr, platform.ErrCoverRelease) {
				result.warn("cover_release_unconfirmed")
			}
			if errors.Is(cleanupErr, ErrCoverStopped) || errors.Is(cleanupErr, platform.ErrUnsafeCoverDirectory) {
				result.warn("cover_unavailable_after_commit")
			}
		}
	}
	return result, nil
}

func (r *CoverImportResult) warn(code string) {
	for _, existing := range r.Warnings {
		if existing == code {
			return
		}
	}
	r.Warnings = append(r.Warnings, code)
}
