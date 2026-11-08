package application

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

func archiveCoverImage(record storage.ManagedCover) backup.Image {
	return backup.Image{ID: record.ID, Path: record.RelativePath, Size: record.Info.ByteSize, SHA256: hex.EncodeToString(record.Info.SHA256[:])}
}

func TestReadBackupArchiveCoverValidationAndFailures(t *testing.T) {
	for _, mode := range []string{"present", "id", "path", "size", "hash", "no-info", "reserved", "missing", "changed", "invalid-png", "read-error", "release", "unknown", "wrong-data", "pre-cancel", "late-cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			body := backupCoverPNG(t, 3)
			if mode == "invalid-png" {
				body = []byte("synthetic invalid PNG")
			}
			record := backupCoverRecord(body)
			expected := archiveCoverImage(record)
			beforeRead := true
			switch mode {
			case "id":
				expected.ID = "other"
			case "path":
				expected.Path = "covers/other.png"
			case "size":
				expected.Size++
			case "hash":
				expected.SHA256 = "other"
			case "no-info":
				record.Info = nil
			case "reserved":
				record.State = "reserved"
			case "pre-cancel":
				cancel()
			default:
				beforeRead = false
			}
			cause := errors.New("synthetic reader failure")
			called := false
			got, err := readBackupArchiveCover(ctx, "synthetic", record, expected,
				func(_ context.Context, root, id string, info platform.CoverFileInfo, max int64) (platform.BackupCoverReadResult, error) {
					called = true
					if root != "synthetic" || id != record.ID || max != backup.MaxImageBytes || info.FileID != record.Info.FileID || info.SHA256 != record.Info.SHA256 {
						t.Fatal("wrong read contract")
					}
					r := platform.BackupCoverReadResult{Status: platform.BackupCoverPresent, Data: body}
					switch mode {
					case "missing":
						return platform.BackupCoverReadResult{Status: platform.BackupCoverMissing}, nil
					case "changed":
						return platform.BackupCoverReadResult{Status: platform.BackupCoverChanged}, nil
					case "read-error":
						return r, cause
					case "release":
						return r, errors.Join(cause, platform.ErrCoverRelease)
					case "unknown":
						return platform.BackupCoverReadResult{}, nil
					case "wrong-data":
						r.Data = []byte("changed")
					case "late-cancel":
						cancel()
					}
					return r, nil
				})
			if called == beforeRead {
				t.Fatal("unexpected read admission", called)
			}
			if mode == "present" {
				if err != nil || !bytes.Equal(got, body) {
					t.Fatal(got, err)
				}
			} else if got != nil || !errors.Is(err, ErrBackupCover) {
				t.Fatal("failure returned image bytes", got, err)
			}
			if (mode == "read-error" || mode == "release") && !errors.Is(err, cause) || mode == "release" && !errors.Is(err, platform.ErrCoverRelease) {
				t.Fatal("cause lost", err)
			}
			if (mode == "pre-cancel" || mode == "late-cancel") && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
		})
	}
}

func TestReadBackupArchiveCoverRevalidatesManagedFile(t *testing.T) {
	for _, mode := range []string{"present", "missing", "changed", "folder-missing"} {
		t.Run(mode, func(t *testing.T) {
			ctx, root, body := context.Background(), t.TempDir(), backupCoverPNG(t, 3)
			record := backupCoverRecord(body)
			info, err := platform.WriteNewCover(ctx, root, record.ID, body)
			if err != nil {
				t.Fatal(err)
			}
			record.Info = &storage.ManagedCoverInfo{VolumeSerial: info.VolumeSerial, FileID: info.FileID, ByteSize: info.ByteSize, SHA256: info.SHA256}
			preflight, err := InspectBackupCover(ctx, root, record)
			if err != nil || preflight.Image == nil {
				t.Fatal(preflight, err)
			}
			name := filepath.Join(root, filepath.FromSlash(record.RelativePath))
			switch mode {
			case "missing":
				err = os.Remove(name)
			case "changed":
				body[0] ^= 1
				err = os.WriteFile(name, body, 0600) // Same file, altered bytes.
			case "folder-missing":
				err = os.Rename(filepath.Join(root, "covers"), filepath.Join(root, "saved-covers"))
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := ReadBackupArchiveCover(ctx, root, record, *preflight.Image)
			if mode == "present" {
				if err != nil || !bytes.Equal(got, body) {
					t.Fatal(got, err)
				}
			} else if got != nil || !errors.Is(err, ErrBackupCover) {
				t.Fatal("post-manifest change became success", got, err)
			}
			if mode == "missing" {
				if _, err := os.Stat(name); !os.IsNotExist(err) {
					t.Fatal("missing image repaired", err)
				}
			} else if mode != "folder-missing" {
				after, err := os.ReadFile(name)
				if err != nil || !bytes.Equal(after, body) {
					t.Fatal("source altered", err)
				}
			}
		})
	}
}
