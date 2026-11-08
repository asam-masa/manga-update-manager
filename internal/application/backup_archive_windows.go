package application

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"

	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

var ErrBackupArchive = errors.New("backup archive preparation or write failed")

// WriteBackupArchive borrows a validated copy and fixed manifest/ready records.
// The caller retains admission, read-stage/reference protection and owned file
// identities/parents through completion. It must discard dst on any error.
// This function neither closes borrowed DB/dst, publishes, nor resumes work.
func WriteBackupArchive(ctx context.Context, dst io.Writer, manifest []byte, db io.ReaderAt, directory string, records []storage.ManagedCover) error {
	return writeBackupArchive(ctx, dst, manifest, db, directory, records, ReadBackupArchiveCover)
}

func writeBackupArchive(ctx context.Context, dst io.Writer, manifest []byte, db io.ReaderAt, directory string, records []storage.ManagedCover,
	read func(context.Context, string, storage.ManagedCover, backup.Image) ([]byte, error),
) (err error) {
	defer func() {
		err = errors.Join(err, ctx.Err())
		if err != nil {
			err = errors.Join(ErrBackupArchive, err)
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	if dst == nil || db == nil || len(records) > backup.MaxImages || int64(len(manifest)) > backup.MaxManifestBytes {
		return ErrBackupArchive
	}
	data := bytes.Clone(manifest)
	m, err := backup.DecodeManifest(bytes.NewReader(data))
	if err != nil {
		return err
	}
	if len(records) != len(m.Images)+len(m.OmittedImages) {
		return ErrBackupArchive
	}
	byPath := make(map[string]storage.ManagedCover, len(records))
	for _, record := range records {
		if record.State != "ready" || record.Info == nil {
			return ErrBackupArchive
		}
		if _, duplicate := byPath[record.RelativePath]; duplicate {
			return ErrBackupArchive
		}
		info := *record.Info
		record.Info = &info // Freeze metadata before any source/output callbacks.
		byPath[record.RelativePath] = record
	}
	images := make(map[string]backup.Image, len(m.Images))
	for _, image := range m.Images {
		record, ok := byPath[image.Path]
		if !ok || record.ID != image.ID || record.Info.ByteSize != image.Size || hex.EncodeToString(record.Info.SHA256[:]) != image.SHA256 {
			return ErrBackupArchive
		}
		images[image.Path] = image
	}
	for _, omitted := range m.OmittedImages {
		if record, ok := byPath[omitted.Path]; !ok || record.ID != omitted.ID {
			return ErrBackupArchive
		}
	}
	return backup.WriteArchive(ctx, dst, data, func(name string) (io.ReadCloser, error) {
		if name == m.Database.Path {
			return io.NopCloser(io.NewSectionReader(db, 0, m.Database.Size+1)), nil
		}
		image, ok := images[name]
		if !ok {
			return nil, ErrBackupArchive
		}
		body, err := read(ctx, directory, byPath[name], image)
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(body)), nil
	})
}
