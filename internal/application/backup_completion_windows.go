package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/asam-masa/manga-update-manager/internal/platform"
)

var ErrBackupCompletion = errors.New("backup publication not confirmed")
var ErrBackupPostprocess = errors.New("backup publication confirmed with postprocessing warning")

type backupPublicationFile interface {
	PublishNoReplace(context.Context) (platform.BackupPublication, error)
	Sync() error
	Close() error
}

// CompleteBackupArchive requires successful preparation and exclusive ownership.
// Confirmed results are synced then closed even if Sync fails. Their errors
// are warnings and never erase the observed publication state.
// Non-confirmed results retain caller ownership for preservation/owned cleanup;
// the caller must eventually Close them and propagate release errors.
// A late cancellation does not undo publication. This function never deletes,
// retries or guarantees durability after power loss or names after Close.
func CompleteBackupArchive(ctx context.Context, file backupPublicationFile) (platform.BackupPublication, error) {
	result, publishErr := file.PublishNoReplace(ctx)
	if result.State != platform.BackupPublishConfirmed {
		return result, errors.Join(ErrBackupCompletion, publishErr)
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(publishErr, syncErr, closeErr); err != nil {
		return result, fmt.Errorf("%w: %w", ErrBackupPostprocess, err)
	}
	return result, nil
}
