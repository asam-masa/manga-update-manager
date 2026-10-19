package application

import (
	"context"
	"errors"
	"io"

	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

// Cleanup consumes the startup ticket reserved before exposing image APIs.
// It never scans files to enroll unknown names or changes work references.
func (a *CoverImporter) Cleanup(ctx context.Context, op *CoverOperation) (warnings []string, err error) {
	warnings = make([]string, 0)
	if op == nil || op.owner != a.operations || op.kind != coverCleanup {
		return warnings, ErrCoverOperationInvalid
	}
	defer func() { err = errors.Join(err, op.End()) }()
	stage := func(fn func() error) error { return a.stage(ctx, op, fn) }
	var unlock func()
	if err = stage(func() error { var e error; unlock, e = a.operations.LockReferences(ctx); return e }); err != nil {
		return warnings, err
	}
	defer unlock()
	var records []storage.ManagedCover
	if err = stage(func() error { var e error; records, e = a.repository.ListManagedCovers(ctx); return e }); err != nil {
		return warnings, err
	}
	paths := make([]string, 0, len(records))
	// Complete every candidate's full reference scan before any file deletion.
	// Retaining the same reference gate prevents application writers interleaving.
	for _, record := range records {
		if record.State == "reserved" {
			if len(warnings) == 0 {
				warnings = append(warnings, "cover_cleanup_unverified")
			}
			continue
		}
		var unused bool
		if err = stage(func() error {
			var e error
			_, unused, e = a.repository.UnusedManagedCover(ctx, record.ID)
			return e
		}); err != nil {
			return warnings, err
		}
		if unused {
			paths = append(paths, record.RelativePath)
		}
	}
	if len(paths) == 0 {
		return warnings, nil
	}
	var guard io.Closer
	if err = stage(func() error { var e error; guard, e = a.prepare(ctx, a.directory); return e }); err != nil {
		return warnings, err
	}
	defer func() {
		if e := guard.Close(); e != nil {
			e = errors.Join(platform.ErrCoverRelease, e)
			a.operations.ObserveError(e)
			err = errors.Join(err, e)
		}
	}()
	for _, path := range paths {
		// Recheck references and identity immediately before the existing deletion.
		if err = a.cleanupPrevious(ctx, path, stage); err != nil {
			return warnings, err
		}
	}
	return warnings, nil
}

func (a *CoverImporter) stage(ctx context.Context, op *CoverOperation, fn func() error) error {
	ticket, err := op.BeginStage()
	if err != nil {
		return err
	}
	err = ctx.Err()
	if err == nil {
		err = fn()
	}
	a.operations.ObserveError(err)
	if errors.Is(err, platform.ErrUnsafeCoverDirectory) {
		a.operations.Stop()
	}
	ticket.End()
	return err
}
