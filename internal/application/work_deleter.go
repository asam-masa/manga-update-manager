package application

import (
	"context"
	"errors"
	"io"

	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

type WorkDeleteRepository interface {
	DeleteWork(context.Context, int64, manga.WorkDeleteInput) (manga.Work, error)
}

type WorkDeleteResult struct {
	Work     *manga.Work // Non-nil only for a confirmed deletion, including warnings.
	Warnings []string
}

type WorkDeleter struct {
	repository WorkDeleteRepository
	covers     *CoverImporter
}

// Both repositories must refer to the same database and share CoverOperations
// with all reference writers. The caller must retain its common work-operation
// reservation and DB lifetime through this call, including cleanup and release.
func NewWorkDeleter(repository WorkDeleteRepository, covers *CoverImporter) *WorkDeleter {
	return &WorkDeleter{repository: repository, covers: covers}
}

func (a *WorkDeleter) DeleteWork(ctx context.Context, id int64, expected manga.WorkDeleteInput) (result WorkDeleteResult, err error) {
	result.Warnings = make([]string, 0)
	// Image-only Stop does not close this gate or authorize any image stage.
	unlock, err := a.covers.operations.LockReferences(ctx)
	if err != nil {
		return result, err
	}
	defer unlock()
	work, err := a.repository.DeleteWork(ctx, id, expected)
	a.covers.operations.ObserveError(err)
	if err != nil && !errors.Is(err, storage.ErrWorkDeleteCommitted) {
		return result, err // Includes missing/changed/unknown: never clean images.
	}
	result.Work = &work
	if err != nil {
		result.Warnings = append(result.Warnings, "database_stopped_after_commit")
		if work.ThumbnailPath != "" {
			result.Warnings = append(result.Warnings, "cover_cleanup_failed")
		}
		return result, nil
	}
	if work.ThumbnailPath == "" {
		return result, nil
	}
	if cleanupErr := a.covers.cleanupDeletedWork(ctx, id, work.ThumbnailPath); cleanupErr != nil {
		result.Warnings = append(result.Warnings, "cover_cleanup_failed")
		if errors.Is(cleanupErr, storage.ErrDBStopped) {
			result.Warnings = append(result.Warnings, "database_stopped_after_commit")
		}
		if errors.Is(cleanupErr, platform.ErrCoverRelease) {
			result.Warnings = append(result.Warnings, "cover_release_unconfirmed")
		}
		if errors.Is(cleanupErr, ErrCoverStopped) || errors.Is(cleanupErr, ErrCoverClosing) || errors.Is(cleanupErr, platform.ErrUnsafeCoverDirectory) {
			result.Warnings = append(result.Warnings, "cover_unavailable_after_commit")
		}
	}
	return result, nil // A cleanup failure never retries or reverses the deletion.
}

// Called only after confirmed deletion, while retaining reference exclusion.
// Reuse the existing ticket, stage, directory and verified-file deletion checks.
func (a *CoverImporter) cleanupDeletedWork(ctx context.Context, id int64, path string) (err error) {
	op, err := a.operations.BeginChange(id, false)
	if err != nil {
		return err
	}
	defer func() {
		if endErr := op.End(); endErr != nil {
			a.operations.Stop()
			err = errors.Join(err, ErrCoverStopped, endErr)
		}
	}()
	var guard io.Closer
	if err = a.stage(ctx, op, func() error { var e error; guard, e = a.prepare(ctx, a.directory); return e }); err != nil {
		return err
	}
	defer func() {
		if closeErr := guard.Close(); closeErr != nil {
			closeErr = errors.Join(platform.ErrCoverRelease, closeErr)
			a.operations.ObserveError(closeErr)
			err = errors.Join(err, closeErr)
		}
	}()
	return a.cleanupPrevious(ctx, path, func(fn func() error) error { return a.stage(ctx, op, fn) })
}
