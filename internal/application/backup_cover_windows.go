package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
	"github.com/asam-masa/manga-update-manager/internal/thumbnail"
)

var ErrBackupCover = errors.New("backup cover inspection failed")

// BackupCoverInspection contains exactly one entry on success, no image bytes.
// It is a preflight observation, not a completed manifest or permission to skip
// revalidation during ZIP writing. A corrupt reason includes changed content.
type BackupCoverInspection struct {
	Image   *backup.Image
	Omitted *backup.OmittedImage
}

// InspectBackupCover consumes a ready record from the validated copy. The caller
// retains admission/reference exclusion throughout inspection and release.
func InspectBackupCover(ctx context.Context, directory string, record storage.ManagedCover) (BackupCoverInspection, error) {
	return inspectBackupCover(ctx, directory, record, platform.ReadBackupCover)
}

func inspectBackupCover(ctx context.Context, directory string, record storage.ManagedCover, read func(context.Context, string, string, platform.CoverFileInfo, int64) (platform.BackupCoverReadResult, error)) (result BackupCoverInspection, err error) {
	defer func() {
		err = errors.Join(err, ctx.Err())
		if err != nil {
			result = BackupCoverInspection{}
			err = errors.Join(ErrBackupCover, err)
		}
	}()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if len(record.ID) != 32 || record.RelativePath != "covers/"+record.ID+".png" || record.State != "ready" || record.Info == nil {
		return result, ErrBackupCover
	}
	for _, c := range []byte(record.ID) {
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			return result, ErrBackupCover
		}
	}
	info := *record.Info
	if info.ByteSize <= 0 || info.ByteSize > backup.MaxImageBytes {
		return result, ErrBackupCover
	}
	observed, err := read(ctx, directory, record.ID, platform.CoverFileInfo{VolumeSerial: info.VolumeSerial, FileID: info.FileID, ByteSize: info.ByteSize, SHA256: info.SHA256}, backup.MaxImageBytes)
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	reason := ""
	switch observed.Status {
	case platform.BackupCoverMissing, platform.BackupCoverChanged:
		if observed.Data != nil {
			return result, ErrBackupCover
		}
		reason = "missing"
		if observed.Status == platform.BackupCoverChanged {
			reason = "corrupt"
		}
	case platform.BackupCoverPresent:
		// A contradictory successful lower result is not a known corrupt image.
		if int64(len(observed.Data)) != info.ByteSize || sha256.Sum256(observed.Data) != info.SHA256 {
			return result, ErrBackupCover
		}
		if err := thumbnail.ValidateCover(observed.Data); err != nil {
			reason = "corrupt"
		}
	default:
		return result, ErrBackupCover
	}
	if reason != "" {
		return BackupCoverInspection{Omitted: &backup.OmittedImage{ID: record.ID, Path: record.RelativePath, Reason: reason}}, nil
	}
	return BackupCoverInspection{Image: &backup.Image{ID: record.ID, Path: record.RelativePath, Size: info.ByteSize, SHA256: hex.EncodeToString(info.SHA256[:])}}, nil
}
