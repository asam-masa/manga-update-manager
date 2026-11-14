package main

import (
	"bytes"
	"context"
	"errors"
	"slices"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

type backupWorkNamesState uint8

const (
	backupWorkNamesUnobserved backupWorkNamesState = iota
	backupWorkNamesAvailable
	backupWorkNamesUnavailable
)

// Diagnostic values only: never use these to write an archive or authorize
// publication, deletion, retry or resuming operations. Unknown is not zero.
type backupOmissionObservation struct {
	Known          bool
	Images         []backup.OmittedImage
	WorkNamesState backupWorkNamesState
	Works          []storage.CopyCoverWork
	DetailError    error // Immutable internal cause, not a public DTO.
}

// The caller retains admission and the same validated copy/parents throughout.
// The manifest is already validated and belongs to that copy. Only a detail-read
// failure with confirmed SQL release and healthy App state may become a warning.
// Later source rechecks, cleanup and publication remain mandatory.
func (a *App) observeBackupOmissions(ctx context.Context, path string, manifest []byte,
	load func(context.Context, string, []string) ([]storage.CopyCoverWork, error),
) (out backupOmissionObservation, err error) {
	m, err := backup.DecodeManifest(bytes.NewReader(manifest))
	if err != nil {
		return out, err
	}
	out.Known, out.Images = true, m.OmittedImages
	if err := a.checkBackupState(ctx); err != nil {
		return out, err
	}
	ids := make([]string, len(out.Images))
	for i, image := range out.Images {
		ids[i] = image.ID
	}
	works, readErr := load(ctx, path, ids)
	stateErr := a.checkBackupState(ctx)
	err = errors.Join(readErr, stateErr, ctx.Err())
	if err != nil {
		out.WorkNamesState, out.DetailError = backupWorkNamesUnavailable, err
		// A generic failure is not automatically an optional-detail failure.
		// Preserve cancellation/stop/release causes even when joined to it.
		if stateErr != nil || !errors.Is(readErr, storage.ErrDatabaseCopyCoverWorks) ||
			errors.Is(err, storage.ErrDatabaseCopyRelease) || errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) || errors.Is(err, storage.ErrDBStopped) ||
			errors.Is(err, application.ErrCoverStopped) || errors.Is(err, application.ErrCoverClosing) {
			return out, err
		}
		return out, nil // DetailError is a retained warning, not archive failure.
	}
	out.WorkNamesState, out.Works = backupWorkNamesAvailable, slices.Clone(works)
	return out, nil
}
