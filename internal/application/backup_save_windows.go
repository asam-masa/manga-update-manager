package application

import (
	"context"
	"errors"
	"io"

	"github.com/asam-masa/manga-update-manager/internal/platform"
)

type backupSaveFile interface {
	backupArchiveFile
	backupPublicationFile
	Path() string
	Discard() (platform.BackupDiscardResult, error)
}

// Cleanup is nil when Discard was not called. Its observations and publication
// paths never authorize reopening/removing a same-named file later.
type BackupSaveResult struct {
	Publication platform.BackupPublication
	Cleanup     *platform.BackupDiscardResult
}

// SaveBackupArchive consumes exclusive ownership of a newly created, empty
// retained file. It prepares then publishes once and attempts all necessary
// releases before returning. The caller must not concurrently use or reuse it.
// Source consistency, file creation, admission control and UI are separate.
// A cleanup success never erases the original failure/cancellation. Unknown
// publication is preserved; late cancellation never removes a published file.
func SaveBackupArchive(ctx context.Context, file backupSaveFile, manifest []byte, open func(string) (io.ReadCloser, error)) (BackupSaveResult, error) {
	_, err := PrepareBackupArchive(ctx, file, manifest, open)
	return FinishBackupArchive(ctx, file, err)
}

// FinishBackupArchive consumes the retained file after preparation and all source
// rechecks/cleanup. A preparation error prevents publication but still requires
// owned cleanup/release. The caller retains admission through this entire call.
// Passing nil requires successful preparation of this same exclusively held file.
func FinishBackupArchive(ctx context.Context, file backupSaveFile, preparationErr error) (BackupSaveResult, error) {
	r := BackupSaveResult{Publication: platform.BackupPublication{TemporaryPath: file.Path()}}
	err := preparationErr
	if err == nil {
		r.Publication, err = CompleteBackupArchive(ctx, file)
		if r.Publication.State == platform.BackupPublishConfirmed {
			return r, err // Complete already attempted Sync and Close.
		}
	}
	if r.Publication.State == platform.BackupPublishNotAttempted || r.Publication.State == platform.BackupPublishCollision {
		cleanup, cleanupErr := file.Discard()
		r.Cleanup = &cleanup
		err = errors.Join(err, cleanupErr)
		if cleanup.MarkedForDeletion {
			return r, err // Discard already attempted Close/check/parent release.
		}
	}
	// Refusal/delete-API failure returns ownership. Unknown (or an unrecognized
	// future state) is preserved without Discard. Never retry native operations.
	return r, errors.Join(err, file.Close())
}
