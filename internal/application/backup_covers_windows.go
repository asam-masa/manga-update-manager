package application

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"sort"

	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

var ErrBackupCovers = errors.New("backup cover inventory failed")

// BackupCoverInventory is metadata only, not a complete manifest. The final
// budget must also include the database and encoded manifest. No image bytes
// are retained, and ZIP writing must revalidate each included image.
type BackupCoverInventory struct {
	Images        []backup.Image
	OmittedImages []backup.OmittedImage
}

// InspectBackupCovers observes referenced ready records from the validated DB
// copy. The caller retains admission/reference exclusion until all release ends.
func InspectBackupCovers(ctx context.Context, directory string, records []storage.ManagedCover) (BackupCoverInventory, error) {
	return inspectBackupCovers(ctx, directory, records, platform.HoldExistingCoverDirectory, InspectBackupCover)
}

func inspectBackupCovers(ctx context.Context, directory string, records []storage.ManagedCover,
	hold func(context.Context, string) (io.Closer, error),
	inspect func(context.Context, string, storage.ManagedCover) (BackupCoverInspection, error),
) (result BackupCoverInventory, err error) {
	defer func() {
		err = errors.Join(err, ctx.Err())
		if err != nil {
			result = BackupCoverInventory{}
			err = errors.Join(ErrBackupCovers, err)
		}
	}()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if len(records) > backup.MaxImages {
		return result, ErrBackupCovers
	}
	ordered := append([]storage.ManagedCover(nil), records...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	for i := 1; i < len(ordered); i++ {
		if ordered[i-1].ID == ordered[i].ID {
			return result, ErrBackupCovers
		}
	}
	guard, err := hold(ctx, directory)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, guard.Close()) }()
	result.Images = make([]backup.Image, 0)
	result.OmittedImages = make([]backup.OmittedImage, 0)
	var total int64
	for _, record := range ordered {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		entry, err := inspect(ctx, directory, record)
		if err != nil {
			return result, err
		}
		if (entry.Image == nil) == (entry.Omitted == nil) {
			return result, ErrBackupCovers
		}
		if entry.Image != nil {
			v := *entry.Image
			if record.Info == nil || v.ID != record.ID || v.Path != record.RelativePath || v.Size != record.Info.ByteSize || v.SHA256 != hex.EncodeToString(record.Info.SHA256[:]) || v.Size <= 0 || v.Size > backup.MaxImageBytes || v.Size > backup.MaxTotalBytes-total {
				return result, ErrBackupCovers
			}
			total += v.Size
			result.Images = append(result.Images, v)
		} else {
			v := *entry.Omitted
			if v.ID != record.ID || v.Path != record.RelativePath || (v.Reason != "missing" && v.Reason != "corrupt") {
				return result, ErrBackupCovers
			}
			result.OmittedImages = append(result.OmittedImages, v)
		}
	}
	return result, nil
}
