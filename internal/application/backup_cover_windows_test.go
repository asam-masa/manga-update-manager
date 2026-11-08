package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
	"github.com/asam-masa/manga-update-manager/internal/thumbnail"
)

func backupCoverPNG(t *testing.T, width int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, width, 4))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func backupCoverRecord(data []byte) storage.ManagedCover {
	id := strings.Repeat("a", 32)
	return storage.ManagedCover{ID: id, RelativePath: "covers/" + id + ".png", State: "ready", Info: &storage.ManagedCoverInfo{VolumeSerial: 19, FileID: [16]byte{23}, ByteSize: int64(len(data)), SHA256: sha256.Sum256(data)}}
}

func TestInspectBackupCoverPNGAndFormatLimits(t *testing.T) {
	valid := backupCoverPNG(t, 3)
	broken := append([]byte(nil), valid...)
	broken[len(broken)-1] ^= 1
	for _, tc := range []struct {
		name    string
		data    []byte
		corrupt bool
	}{
		{"valid", valid, false},
		{"max-side", backupCoverPNG(t, thumbnail.MaxSide), false},
		{"wide", backupCoverPNG(t, thumbnail.MaxSide+1), true},
		{"not-png", []byte("synthetic"), true},
		{"truncated", valid[:len(valid)-5], true},
		{"crc", broken, true},
		{"trailing", append(append([]byte(nil), valid...), 0), true},
		{"png-rule-size", make([]byte, 2*1024*1024+1), true},
		{"format-limit", make([]byte, backup.MaxImageBytes), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := backupCoverRecord(tc.data)
			got, err := inspectBackupCover(context.Background(), "synthetic", record, func(_ context.Context, dir, id string, info platform.CoverFileInfo, max int64) (platform.BackupCoverReadResult, error) {
				if dir != "synthetic" || id != record.ID || max != backup.MaxImageBytes || info.VolumeSerial != record.Info.VolumeSerial || info.FileID != record.Info.FileID || info.ByteSize != record.Info.ByteSize || info.SHA256 != record.Info.SHA256 {
					t.Fatal("wrong reader contract")
				}
				return platform.BackupCoverReadResult{Status: platform.BackupCoverPresent, Data: tc.data}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if tc.corrupt {
				want := &backup.OmittedImage{ID: record.ID, Path: record.RelativePath, Reason: "corrupt"}
				if got.Image != nil || !reflect.DeepEqual(got.Omitted, want) {
					t.Fatal(got)
				}
			} else {
				want := &backup.Image{ID: record.ID, Path: record.RelativePath, Size: record.Info.ByteSize, SHA256: hex.EncodeToString(record.Info.SHA256[:])}
				if got.Omitted != nil || !reflect.DeepEqual(got.Image, want) {
					t.Fatal(got)
				}
			}
		})
	}
}

func TestInspectBackupCoverFailureIsNotOmission(t *testing.T) {
	data := backupCoverPNG(t, 3)
	fault := errors.New("synthetic read failure")
	for _, mode := range []string{"reserved", "no-info", "id", "upper-id", "path", "zero-size", "over-limit", "pre-cancel", "read-error", "release", "unknown", "missing-data", "changed-data", "wrong-size", "wrong-hash", "late-cancel"} {
		t.Run(mode, func(t *testing.T) {
			record := backupCoverRecord(data)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			beforeRead := true
			switch mode {
			case "reserved":
				record.State = "reserved"
			case "no-info":
				record.Info = nil
			case "id":
				record.ID = "../x"
			case "upper-id":
				record.ID = strings.ToUpper(record.ID)
				record.RelativePath = "covers/" + record.ID + ".png"
			case "path":
				record.RelativePath = "covers/other.png"
			case "zero-size":
				record.Info.ByteSize = 0
			case "over-limit":
				record.Info.ByteSize = backup.MaxImageBytes + 1
			case "pre-cancel":
				cancel()
			default:
				beforeRead = false
			}
			calls := 0
			got, err := inspectBackupCover(ctx, "synthetic", record, func(context.Context, string, string, platform.CoverFileInfo, int64) (platform.BackupCoverReadResult, error) {
				calls++
				out := platform.BackupCoverReadResult{Status: platform.BackupCoverPresent, Data: data}
				switch mode {
				case "read-error":
					return platform.BackupCoverReadResult{Status: platform.BackupCoverMissing}, fault
				case "release":
					return out, errors.Join(fault, platform.ErrCoverRelease)
				case "unknown":
					out.Status = "unknown"
				case "missing-data":
					out.Status = platform.BackupCoverMissing
				case "changed-data":
					out.Status = platform.BackupCoverChanged
				case "wrong-size":
					out.Data = data[:len(data)-1]
				case "wrong-hash":
					out.Data = append([]byte(nil), data...)
					out.Data[0] ^= 1
				case "late-cancel":
					cancel()
				}
				return out, nil
			})
			if !errors.Is(err, ErrBackupCover) || got.Image != nil || got.Omitted != nil {
				t.Fatal("failure classified as omission", got, err)
			}
			if (calls == 0) != beforeRead {
				t.Fatal("unexpected read", calls)
			}
			if (mode == "read-error" || mode == "release") && !errors.Is(err, fault) {
				t.Fatal("cause lost", err)
			}
			if mode == "release" && !errors.Is(err, platform.ErrCoverRelease) {
				t.Fatal(err)
			}
			if strings.Contains(mode, "cancel") && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}

func TestInspectBackupCoverRealFiles(t *testing.T) {
	for _, mode := range []string{"present", "missing", "changed", "invalid-png", "folder-missing"} {
		t.Run(mode, func(t *testing.T) {
			dir, data := t.TempDir(), backupCoverPNG(t, 3)
			if mode == "invalid-png" {
				data = []byte("synthetic broken PNG")
			}
			record := backupCoverRecord(data)
			info, err := platform.WriteNewCover(context.Background(), dir, record.ID, data)
			if err != nil {
				t.Fatal(err)
			}
			record.Info = &storage.ManagedCoverInfo{VolumeSerial: info.VolumeSerial, FileID: info.FileID, ByteSize: info.ByteSize, SHA256: info.SHA256}
			name := filepath.Join(dir, filepath.FromSlash(record.RelativePath))
			switch mode {
			case "missing":
				err = os.Remove(name)
			case "changed":
				data[0] ^= 1
				err = os.WriteFile(name, data, 0600)
			case "folder-missing":
				err = os.Rename(filepath.Join(dir, "covers"), filepath.Join(dir, "saved-covers"))
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := InspectBackupCover(context.Background(), dir, record)
			if mode == "folder-missing" {
				if err == nil || got.Image != nil || got.Omitted != nil {
					t.Fatal(got, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "present" {
				if got.Image == nil || got.Omitted != nil {
					t.Fatal(got)
				}
			} else {
				reason := "corrupt"
				if mode == "missing" {
					reason = "missing"
				}
				if got.Image != nil || got.Omitted == nil || got.Omitted.Reason != reason {
					t.Fatal(got)
				}
			}
			if mode != "missing" {
				after, err := os.ReadFile(name)
				if err != nil || !bytes.Equal(after, data) {
					t.Fatal("source changed", err)
				}
			}
		})
	}
}
