package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/backup"
)

func backupPrepareFixture(t *testing.T) ([]byte, []byte) {
	t.Helper()
	data := []byte("synthetic database")
	hash := sha256.Sum256(data)
	m := backup.Manifest{FormatVersion: 1, CreatedAt: "2026-11-04T00:00:00.000000000Z", AppVersion: "fixture", Migrations: []string{"001_create_works.sql"}, Database: backup.File{Path: "database.sqlite", Size: int64(len(data)), SHA256: hex.EncodeToString(hash[:])}, Images: []backup.Image{}, OmittedImages: []backup.OmittedImage{}}
	encoded, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return encoded, data
}

type backupPrepareMemory struct {
	bytes.Buffer
	stage         string
	cause         error
	log           []string
	identityCalls int
	sizeCalls     int
	cancel        context.CancelFunc
}

func (f *backupPrepareMemory) Write(p []byte) (int, error) {
	if len(f.log) == 0 || f.log[len(f.log)-1] != "write" {
		f.log = append(f.log, "write")
	}
	if f.stage == "write" {
		return 0, f.cause
	}
	return f.Buffer.Write(p)
}
func (f *backupPrepareMemory) Sync() error {
	f.log = append(f.log, "sync")
	if f.stage == "sync" {
		return f.cause
	}
	if f.stage == "cancel-sync" {
		f.cancel()
	}
	if f.stage == "corrupt" {
		f.Bytes()[0] ^= 0xff
	}
	return nil
}
func (f *backupPrepareMemory) Size() (int64, error) {
	f.sizeCalls++
	f.log = append(f.log, "size")
	if f.stage == "size" && f.sizeCalls == 2 {
		return 0, f.cause
	}
	return int64(f.Len()), nil
}
func (f *backupPrepareMemory) ReadAt(p []byte, offset int64) (int, error) {
	if f.log[len(f.log)-1] != "read" {
		f.log = append(f.log, "read")
	}
	if f.stage == "read" {
		return 0, f.cause
	}
	if f.stage == "cancel-read" {
		f.cancel()
	}
	return bytes.NewReader(f.Bytes()).ReadAt(p, offset)
}
func (f *backupPrepareMemory) VerifyIdentity() error {
	f.identityCalls++
	f.log = append(f.log, "identity")
	if f.stage == "identity" && f.identityCalls == 2 {
		return f.cause
	}
	return nil
}

func TestPrepareBackupArchiveOrdersWriteSyncReadAndIdentity(t *testing.T) {
	manifest, data := backupPrepareFixture(t)
	f := &backupPrepareMemory{}
	m, err := PrepareBackupArchive(context.Background(), f, manifest, func(string) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil })
	if err != nil || m.Database.Size != int64(len(data)) {
		t.Fatalf("prepare: %#v %v", m, err)
	}
	want := []string{"identity", "size", "write", "sync", "size", "read", "identity"}
	if !reflect.DeepEqual(f.log, want) {
		t.Fatalf("order %v want %v", f.log, want)
	}
}

func TestPrepareBackupArchiveFailuresReturnNoManifest(t *testing.T) {
	manifest, data := backupPrepareFixture(t)
	for _, stage := range []string{"write", "sync", "size", "read", "identity", "corrupt", "cancel-sync", "cancel-read", "source"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cause := errors.New("synthetic " + stage)
			f := &backupPrepareMemory{stage: stage, cause: cause, cancel: cancel}
			m, err := PrepareBackupArchive(ctx, f, manifest, func(string) (io.ReadCloser, error) {
				if stage == "source" {
					return nil, cause
				}
				return io.NopCloser(bytes.NewReader(data)), nil
			})
			if !errors.Is(err, ErrBackupPrepare) || !reflect.DeepEqual(m, backup.Manifest{}) {
				t.Fatalf("partial result: %#v %v", m, err)
			}
			switch stage {
			case "corrupt":
				if !errors.Is(err, backup.ErrArchive) {
					t.Fatal(err)
				}
			case "cancel-sync", "cancel-read":
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			default:
				if !errors.Is(err, cause) {
					t.Fatal(err)
				}
			}
			if stage == "write" || stage == "source" {
				for _, step := range f.log {
					if step == "sync" || step == "read" {
						t.Fatalf("continued after failure: %v", f.log)
					}
				}
			}
			if stage == "sync" || stage == "cancel-sync" {
				for _, step := range f.log {
					if step == "read" {
						t.Fatalf("read before sync succeeded: %v", f.log)
					}
				}
			}
		})
	}
}

func TestPrepareBackupArchiveRejectsNonemptyAndEarlyCancellation(t *testing.T) {
	manifest, _ := backupPrepareFixture(t)
	for _, cancelled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		if cancelled {
			cancel()
		}
		f := &backupPrepareMemory{}
		f.Buffer.WriteString("keep")
		_, err := PrepareBackupArchive(ctx, f, manifest, func(string) (io.ReadCloser, error) { t.Fatal("opened source"); return nil, nil })
		cancel()
		if !errors.Is(err, ErrBackupPrepare) || f.String() != "keep" {
			t.Fatalf("changed nonempty file: %v", err)
		}
		if cancelled && (!errors.Is(err, context.Canceled) || len(f.log) != 0) {
			t.Fatalf("cancelled file accessed: %v %v", f.log, err)
		}
	}
}
