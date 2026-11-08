package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/backup"
)

var ErrBackupManifest = errors.New("backup manifest preparation failed")

type BackupManifestMetadata struct {
	CreatedAt  time.Time
	AppVersion string
	Migrations []string
	Covers     BackupCoverInventory
}

// BuildBackupManifest borrows an owned, validated copy and bounded metadata from
// the preceding inspection steps. The caller retains admission, copy identity
// and parents throughout. The reader is not closed or retained. Format validity
// is not proof of DB/image correspondence, restoration safety or publication.
func BuildBackupManifest(ctx context.Context, reader io.ReaderAt, size int64, metadata BackupManifestMetadata) (data []byte, err error) {
	defer func() {
		err = errors.Join(err, ctx.Err())
		if err != nil {
			data = nil
			err = errors.Join(ErrBackupManifest, err)
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if reader == nil || size <= 0 || size > backup.MaxDatabaseBytes {
		return nil, backup.ErrManifest
	}
	m := backup.Manifest{
		FormatVersion: 1,
		CreatedAt:     metadata.CreatedAt.UTC().Format(time.RFC3339Nano),
		AppVersion:    metadata.AppVersion,
		Migrations:    metadata.Migrations,
		Database:      backup.File{Path: "database.sqlite", Size: size, SHA256: strings.Repeat("0", 64)},
		Images:        append([]backup.Image{}, metadata.Covers.Images...),
		OmittedImages: append([]backup.OmittedImage{}, metadata.Covers.OmittedImages...),
	}
	// Validate all metadata and the encoded-manifest-inclusive budget before I/O.
	// A fixed-length placeholder has exactly the same JSON size as the digest.
	data, err = json.Marshal(m)
	if err != nil {
		return nil, err
	}
	m, err = backup.DecodeManifest(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	n, err := io.CopyBuffer(h, backupManifestReader{ctx: ctx, Reader: io.NewSectionReader(reader, 0, size+1)}, make([]byte, 32<<10))
	if err != nil {
		return nil, err
	}
	if n != size {
		return nil, backup.ErrManifest
	}
	m.Database.SHA256 = hex.EncodeToString(h.Sum(nil))
	data, err = json.Marshal(m)
	return data, err
}

type backupManifestReader struct {
	ctx context.Context
	io.Reader
}

func (r backupManifestReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}
