package application

import (
	"context"
	"encoding/hex"
	"strings"

	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

// Caller holds the operation ticket, reference exclusion and directory guard.
// stage checks stopping before each new file/DB operation and observes errors.
func (a *CoverImporter) cleanupPrevious(ctx context.Context, path string, stage func(func() error) error) error {
	if len(path) != len("covers/")+32+len(".png") || !strings.HasPrefix(path, "covers/") || !strings.HasSuffix(path, ".png") {
		return storage.ErrInvalidManagedCover
	}
	id := strings.TrimSuffix(strings.TrimPrefix(path, "covers/"), ".png")
	if _, err := hex.DecodeString(id); err != nil || id != strings.ToLower(id) {
		return storage.ErrInvalidManagedCover
	}
	var record storage.ManagedCover
	var unused bool
	if err := stage(func() error {
		var err error
		record, unused, err = a.repository.UnusedManagedCover(ctx, id)
		return err
	}); err != nil {
		return err
	}
	if !unused {
		return nil
	} // Another work still needs this image.
	if record.ID != id || record.RelativePath != path || record.State != "ready" || record.Info == nil {
		return storage.ErrInvalidManagedCover
	}
	info := record.Info
	if err := stage(func() error {
		return a.delete(ctx, a.directory, id, platform.CoverFileInfo{VolumeSerial: info.VolumeSerial, FileID: info.FileID, ByteSize: info.ByteSize, SHA256: info.SHA256})
	}); err != nil {
		return err
	}
	return stage(func() error { return a.repository.ForgetUnusedManagedCover(ctx, record) })
}
