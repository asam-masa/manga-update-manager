package application

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

func applicationArchiveFixture(t *testing.T) ([]byte, []byte, []byte, []storage.ManagedCover) {
	t.Helper()
	db, body := []byte("synthetic validated DB bytes"), backupCoverPNG(t, 3)
	included := backupCoverRecord(body)
	omitted := backupCoverRecord(body)
	omitted.ID, omitted.RelativePath = strings.Repeat("b", 32), "covers/"+strings.Repeat("b", 32)+".png"
	metadata := manifestMetadata()
	metadata.Covers = BackupCoverInventory{Images: []backup.Image{archiveCoverImage(included)}, OmittedImages: []backup.OmittedImage{{ID: omitted.ID, Path: omitted.RelativePath, Reason: "missing"}}}
	manifest, err := BuildBackupManifest(context.Background(), bytes.NewReader(db), int64(len(db)), metadata)
	if err != nil {
		t.Fatal(err)
	}
	return db, body, manifest, []storage.ManagedCover{included, omitted}
}

func TestWriteBackupArchiveRoundTripAndFrozenMetadata(t *testing.T) {
	db, body, manifest, records := applicationArchiveFixture(t)
	var out bytes.Buffer
	reader := manifestReaderAt(func(p []byte, off int64) (int, error) {
		// Input changes during I/O must not rewrite the validated snapshot.
		records[0].Info.ByteSize = 999
		manifest[0] = '!'
		return bytes.NewReader(db).ReadAt(p, off)
	})
	reads := 0
	err := writeBackupArchive(context.Background(), &out, manifest, reader, "synthetic", records,
		func(_ context.Context, root string, record storage.ManagedCover, expected backup.Image) ([]byte, error) {
			reads++
			if root != "synthetic" || record.ID != strings.Repeat("a", 32) || record.Info.ByteSize != int64(len(body)) || expected.Size != int64(len(body)) {
				t.Fatal("wrong image or mutable metadata", record, expected)
			}
			return body, nil
		})
	if err != nil || reads != 1 {
		t.Fatal(reads, err)
	}
	m, err := backup.ValidateArchive(bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil || len(m.Images) != 1 || len(m.OmittedImages) != 1 {
		t.Fatal(m, err)
	}
	z, err := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil || len(z.File) != 3 {
		t.Fatal(err)
	}
	for _, f := range z.File {
		if f.Name == "database.sqlite" || f.Name == m.Images[0].Path {
			r, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(r)
			if err := errors.Join(err, r.Close()); err != nil {
				t.Fatal(err)
			}
			want := body
			if f.Name == "database.sqlite" {
				want = db
			}
			if !bytes.Equal(got, want) {
				t.Fatal("ZIP content mismatch", f.Name)
			}
		}
	}
}

type archiveApplicationWriter func([]byte) (int, error)

func (w archiveApplicationWriter) Write(p []byte) (int, error) { return w(p) }

func TestWriteBackupArchiveFailures(t *testing.T) {
	for _, mode := range []string{"manifest", "count", "duplicate", "id", "size", "hash", "no-info", "state", "db-change", "image-change", "read-error", "release", "output-error", "early-cancel", "late-cancel"} {
		t.Run(mode, func(t *testing.T) {
			db, body, manifest, records := applicationArchiveFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cause := errors.New("synthetic archive failure")
			beforeWrite := true
			switch mode {
			case "manifest":
				manifest = []byte("{}")
			case "count":
				records = records[:1]
			case "duplicate":
				records[1] = records[0]
			case "id":
				records[0].ID = strings.Repeat("c", 32)
			case "size":
				records[0].Info.ByteSize++
			case "hash":
				records[0].Info.SHA256[0] ^= 1
			case "no-info":
				records[0].Info = nil
			case "state":
				records[0].State = "reserved"
			case "early-cancel":
				cancel()
			default:
				beforeWrite = false
			}
			if mode == "db-change" {
				db[0] ^= 1
			}
			var out bytes.Buffer
			writer := archiveApplicationWriter(func(p []byte) (int, error) {
				if mode == "output-error" {
					return 0, cause
				}
				return out.Write(p)
			})
			err := writeBackupArchive(ctx, writer, manifest, bytes.NewReader(db), "synthetic", records,
				func(context.Context, string, storage.ManagedCover, backup.Image) ([]byte, error) {
					switch mode {
					case "read-error":
						return nil, cause
					case "release":
						return nil, errors.Join(cause, platform.ErrCoverRelease)
					case "image-change":
						return []byte("changed"), nil
					case "late-cancel":
						cancel()
					}
					return body, nil
				})
			if !errors.Is(err, ErrBackupArchive) || beforeWrite && out.Len() != 0 {
				t.Fatal("failure accepted or early output", out.Len(), err)
			}
			if (mode == "read-error" || mode == "release" || mode == "output-error") && !errors.Is(err, cause) || mode == "release" && !errors.Is(err, platform.ErrCoverRelease) {
				t.Fatal("cause lost", err)
			}
			if strings.Contains(mode, "cancel") && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
		})
	}
}

func TestWriteBackupArchiveRealImageRevalidation(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "present", true: "missing-after-manifest"}[missing], func(t *testing.T) {
			db, body, manifest, records := applicationArchiveFixture(t)
			root := t.TempDir()
			info, err := platform.WriteNewCover(context.Background(), root, records[0].ID, body)
			if err != nil {
				t.Fatal(err)
			}
			records[0].Info = &storage.ManagedCoverInfo{VolumeSerial: info.VolumeSerial, FileID: info.FileID, ByteSize: info.ByteSize, SHA256: info.SHA256}
			if missing {
				if err := os.Remove(filepath.Join(root, filepath.FromSlash(records[0].RelativePath))); err != nil {
					t.Fatal(err)
				}
			}
			var out bytes.Buffer
			err = WriteBackupArchive(context.Background(), &out, manifest, bytes.NewReader(db), root, records)
			if missing {
				if !errors.Is(err, ErrBackupCover) {
					t.Fatal(err)
				}
			} else if _, validationErr := backup.ValidateArchive(bytes.NewReader(out.Bytes()), int64(out.Len())); err != nil || validationErr != nil {
				t.Fatal(err, validationErr)
			}
		})
	}
}
