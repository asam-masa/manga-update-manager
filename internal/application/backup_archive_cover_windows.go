package application

import (
	"context"
	"encoding/hex"
	"errors"

	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

// ReadBackupArchiveCover returns one revalidated image for ZIP writing. The
// caller retains normal admission, reference exclusion and the validated copy.
// This helper does not acquire operation tickets or publish/mutate a manifest.
// A missing/changed image after manifest creation is an error, never omission.
func ReadBackupArchiveCover(ctx context.Context, directory string, record storage.ManagedCover, expected backup.Image) ([]byte, error) {
	return readBackupArchiveCover(ctx, directory, record, expected, platform.ReadBackupCover)
}

func readBackupArchiveCover(ctx context.Context, directory string, record storage.ManagedCover, expected backup.Image,
	read func(context.Context, string, string, platform.CoverFileInfo, int64) (platform.BackupCoverReadResult, error),
) (data []byte, err error) {
	defer func() {
		err = errors.Join(err, ctx.Err())
		if err != nil {
			data = nil
			err = errors.Join(ErrBackupCover, err)
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if record.Info == nil || expected != (backup.Image{ID: record.ID, Path: record.RelativePath, Size: record.Info.ByteSize, SHA256: hex.EncodeToString(record.Info.SHA256[:])}) {
		return nil, ErrBackupCover
	}
	inspection, err := inspectBackupCover(ctx, directory, record,
		func(ctx context.Context, directory, id string, info platform.CoverFileInfo, max int64) (platform.BackupCoverReadResult, error) {
			observed, err := read(ctx, directory, id, info, max)
			data = observed.Data // One bounded image; discard on any later failure.
			return observed, err
		})
	if err != nil {
		return nil, err
	}
	if inspection.Image == nil || *inspection.Image != expected {
		return nil, ErrBackupCover
	}
	return data, nil
}
