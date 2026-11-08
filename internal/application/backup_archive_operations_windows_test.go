package application

import (
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

func TestBackupArchiveOperationsStagesAndRelease(t *testing.T) {
	for _, mode := range []string{"success", "empty", "stopped", "closing", "pre-cancel", "hold-error", "unsafe", "stop-hold", "stop-db", "stop-image", "stop-output", "stop-close", "cancel-image", "cancel-close", "read-error", "read-release", "output-error", "close-error", "close-release"} {
		t.Run(mode, func(t *testing.T) {
			db, body, manifest, records := applicationArchiveFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var operations CoverOperations
			if mode == "stopped" {
				operations.Stop()
			}
			if mode == "closing" {
				if _, err := operations.RequestClose(); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "pre-cancel" {
				cancel()
			}
			if mode == "empty" {
				var err error
				manifest, err = BuildBackupManifest(ctx, bytes.NewReader(db), int64(len(db)), manifestMetadata())
				if err != nil {
					t.Fatal(err)
				}
				records = nil
			}
			checkIO := func() {
				t.Helper()
				assertBackupReadOwnership(t, &operations, 1)
				operations.mu.Lock()
				stopped := operations.stopped
				operations.mu.Unlock()
				if stopped {
					t.Fatal("new lower I/O after Stop")
				}
			}
			cause := errors.New("synthetic archive operation failure")
			held, closed, reads := false, false, 0
			hold := func(context.Context, string) (io.Closer, error) {
				checkIO()
				if mode == "hold-error" {
					return nil, cause
				}
				if mode == "unsafe" {
					return nil, errors.Join(cause, platform.ErrUnsafeCoverDirectory)
				}
				held = true
				if mode == "stop-hold" {
					operations.Stop()
				}
				return inventoryCloseFunc(func() error {
					assertBackupReadOwnership(t, &operations, 1)
					closed = true
					if mode == "read-release" && !errors.Is(operations.CheckIdle(), ErrCoverStopped) {
						t.Error("release mark not observed before parent close")
					}
					if mode == "stop-close" {
						operations.Stop()
					}
					if mode == "cancel-close" {
						cancel()
					}
					if mode == "close-release" {
						return errors.Join(cause, platform.ErrCoverRelease)
					}
					if mode == "close-error" {
						return cause
					}
					return nil
				}), nil
			}
			var out bytes.Buffer
			writer := archiveApplicationWriter(func(p []byte) (int, error) {
				checkIO()
				if mode == "output-error" {
					return 0, cause
				}
				if mode == "stop-output" {
					operations.Stop()
				}
				return out.Write(p)
			})
			source := manifestReaderAt(func(p []byte, off int64) (int, error) {
				checkIO()
				if mode == "stop-db" {
					operations.Stop()
				}
				return bytes.NewReader(db).ReadAt(p, off)
			})
			read := func(context.Context, string, storage.ManagedCover, backup.Image) ([]byte, error) {
				checkIO()
				reads++
				if mode == "read-error" {
					return nil, cause
				}
				if mode == "read-release" {
					return nil, errors.Join(cause, platform.ErrCoverRelease)
				}
				if mode == "stop-image" {
					operations.Stop()
				}
				if mode == "cancel-image" {
					cancel()
				}
				return body, nil
			}
			err := writeBackupArchiveWithOperations(ctx, writer, manifest, source, "synthetic", records, &operations, hold, read)
			if (err == nil) != (mode == "success" || mode == "empty") || held != closed {
				t.Fatal(held, closed, err)
			}
			assertBackupReadOwnership(t, &operations, 0)
			if (mode == "stop-db" || mode == "stop-hold" || mode == "empty") && reads != 0 {
				t.Fatal("unexpected image read", reads)
			}
			if mode == "success" || mode == "empty" {
				if !held {
					t.Fatal("existing directory not held, including empty inventory")
				}
				if _, err := backup.ValidateArchive(bytes.NewReader(out.Bytes()), int64(out.Len())); err != nil {
					t.Fatal(err)
				}
			}
			wantStopped := mode == "stopped" || strings.HasPrefix(mode, "stop-") || mode == "unsafe" || mode == "read-release" || mode == "close-release"
			operations.mu.Lock()
			stopped := operations.stopped
			operations.mu.Unlock()
			if stopped != wantStopped {
				t.Fatal("wrong stop notification", stopped, wantStopped)
			}
			if (mode == "read-error" || mode == "read-release" || mode == "output-error" || mode == "hold-error" || mode == "unsafe" || mode == "close-error" || mode == "close-release") && !errors.Is(err, cause) {
				t.Fatal("cause lost", err)
			}
			if (mode == "pre-cancel" || mode == "cancel-image" || mode == "cancel-close") && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
		})
	}
}

func TestBackupArchiveOperationsRejectsMissingDependencies(t *testing.T) {
	if err := WriteBackupArchiveWithOperations(context.Background(), io.Discard, nil, bytes.NewReader(nil), "", nil, nil); !errors.Is(err, ErrBackupArchive) {
		t.Fatal(err)
	}
}

func TestBackupArchiveOperationsRealImage(t *testing.T) {
	db, body, manifest, records := applicationArchiveFixture(t)
	ctx, root := context.Background(), t.TempDir()
	info, err := platform.WriteNewCover(ctx, root, records[0].ID, body)
	if err != nil {
		t.Fatal(err)
	}
	records[0].Info = &storage.ManagedCoverInfo{VolumeSerial: info.VolumeSerial, FileID: info.FileID, ByteSize: info.ByteSize, SHA256: info.SHA256}
	var operations CoverOperations
	var out bytes.Buffer
	if err := WriteBackupArchiveWithOperations(ctx, &out, manifest, bytes.NewReader(db), root, records, &operations); err != nil {
		t.Fatal(err)
	}
	if _, err := backup.ValidateArchive(bytes.NewReader(out.Bytes()), int64(out.Len())); err != nil {
		t.Fatal(err)
	}
	assertBackupReadOwnership(t, &operations, 0)
	if err := operations.CheckIdle(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "covers"), filepath.Join(root, "moved")); err != nil {
		t.Fatal("parent still held", err)
	}
}
