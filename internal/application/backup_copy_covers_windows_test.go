package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

func assertBackupReadOwnership(t *testing.T, operations *CoverOperations, want int) {
	t.Helper()
	operations.mu.Lock()
	active, references := len(operations.active), len(operations.references)
	operations.mu.Unlock()
	if active != want || references != want {
		t.Fatalf("read/reference ownership: %d/%d want %d", active, references, want)
	}
}

func TestInspectBackupCopyCoversStagesAndRelease(t *testing.T) {
	for _, mode := range []string{"success", "empty", "stopped", "closing", "load-error", "sql-release", "stop-load", "hold-error", "unsafe", "read-error", "read-release", "stop-first", "stop-last", "cancel-first", "close-error", "cancel-close", "empty-stop"} {
		t.Run(mode, func(t *testing.T) {
			var operations CoverOperations
			if mode == "stopped" {
				operations.Stop()
			}
			if mode == "closing" {
				if _, err := operations.RequestClose(); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cause := errors.New("synthetic copy inspection failure")
			loads, holds, reads, closes := 0, 0, 0, 0
			got, err := inspectBackupCopyCovers(ctx, "copy.sqlite", "source", &operations,
				func(_ context.Context, path string, max int) ([]storage.ManagedCover, error) {
					loads++
					assertBackupReadOwnership(t, &operations, 1)
					if path != "copy.sqlite" || max != backup.MaxImages {
						t.Fatal(path, max)
					}
					switch mode {
					case "empty", "empty-stop":
						return nil, nil
					case "load-error":
						return nil, cause
					case "sql-release":
						return nil, errors.Join(cause, storage.ErrDatabaseCopyRelease)
					case "stop-load":
						operations.Stop()
					}
					return []storage.ManagedCover{inventoryRecord(1, 1), inventoryRecord(2, 1)}, nil
				},
				func(_ context.Context, directory string) (io.Closer, error) {
					holds++
					assertBackupReadOwnership(t, &operations, 1)
					if directory != "source" {
						t.Fatal(directory)
					}
					if mode == "hold-error" {
						return nil, cause
					}
					if mode == "unsafe" {
						return nil, platform.ErrUnsafeCoverDirectory
					}
					if mode == "empty-stop" {
						operations.Stop()
					}
					return inventoryCloseFunc(func() error {
						closes++
						assertBackupReadOwnership(t, &operations, 1)
						if mode == "close-error" {
							return errors.Join(cause, platform.ErrCoverRelease)
						}
						if mode == "cancel-close" {
							cancel()
						}
						return nil
					}), nil
				},
				func(_ context.Context, _ string, r storage.ManagedCover) (BackupCoverInspection, error) {
					reads++
					assertBackupReadOwnership(t, &operations, 1)
					if mode == "read-error" {
						return BackupCoverInspection{}, cause
					}
					if mode == "read-release" {
						return BackupCoverInspection{}, errors.Join(cause, platform.ErrCoverRelease)
					}
					if mode == "stop-first" || mode == "stop-last" && reads == 2 {
						operations.Stop()
					}
					if mode == "cancel-first" {
						cancel()
					}
					return inventoryImage(r), nil
				})
			assertBackupReadOwnership(t, &operations, 0)
			success := mode == "success" || mode == "empty"
			if success {
				want := 2
				if mode == "empty" {
					want = 0
				}
				if err != nil || got.Images == nil || got.OmittedImages == nil || len(got.Images) != want {
					t.Fatal(got, err)
				}
			} else if err == nil || got.Images != nil || got.OmittedImages != nil {
				t.Fatal("partial result", got, err)
			}
			wantLoads, wantHolds, wantReads, wantCloses := 1, 1, 2, 1
			switch mode {
			case "stopped", "closing":
				wantLoads, wantHolds, wantReads, wantCloses = 0, 0, 0, 0
			case "load-error", "sql-release", "stop-load":
				wantHolds, wantReads, wantCloses = 0, 0, 0
			case "hold-error", "unsafe":
				wantReads, wantCloses = 0, 0
			case "empty", "empty-stop":
				wantReads = 0
			case "read-error", "read-release", "stop-first", "cancel-first":
				wantReads = 1
			}
			if loads != wantLoads || holds != wantHolds || reads != wantReads || closes != wantCloses {
				t.Fatalf("calls %d/%d/%d/%d want %d/%d/%d/%d", loads, holds, reads, closes, wantLoads, wantHolds, wantReads, wantCloses)
			}
			stopped := mode == "stopped" || mode == "stop-load" || mode == "unsafe" || mode == "read-release" || mode == "stop-first" || mode == "stop-last" || mode == "close-error" || mode == "empty-stop"
			if errors.Is(operations.CheckIdle(), ErrCoverStopped) != stopped {
				t.Fatal("wrong image stop state", operations.CheckIdle())
			}
			if (mode == "stopped" || mode == "stop-load" || mode == "stop-first" || mode == "stop-last" || mode == "empty-stop") && !errors.Is(err, ErrCoverStopped) {
				t.Fatal("stop cause lost", err)
			}
			if mode == "closing" && !errors.Is(err, ErrCoverClosing) {
				t.Fatal(err)
			}
			if mode == "unsafe" && !errors.Is(err, platform.ErrUnsafeCoverDirectory) {
				t.Fatal(err)
			}
			if (mode == "read-release" || mode == "close-error") && !errors.Is(err, platform.ErrCoverRelease) {
				t.Fatal("release mark lost", err)
			}
			if mode == "sql-release" && !errors.Is(err, storage.ErrDatabaseCopyRelease) {
				t.Fatal("SQL mark lost", err)
			}
			if (mode == "cancel-first" || mode == "cancel-close") && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if (mode == "load-error" || mode == "sql-release" || mode == "hold-error" || mode == "read-error" || mode == "read-release" || mode == "close-error") && !errors.Is(err, cause) {
				t.Fatal("cause lost", err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := inspectBackupCopyCovers(ctx, "", "", nil, nil, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := InspectBackupCopyCovers(context.Background(), "", "", nil); !errors.Is(err, ErrBackupCovers) {
		t.Fatal(err)
	}
}

func TestInspectBackupCopyCoversUsesCopyAndManagedImages(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	source, err := storage.Open(ctx, filepath.Join(directory, "source.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	add := func(id int) storage.ManagedCover {
		r := inventoryRecord(id, 1)
		data := backupCoverPNG(t, 3)
		info, err := platform.WriteNewCover(ctx, directory, r.ID, data)
		if err != nil {
			t.Fatal(err)
		}
		if err := source.ReserveManagedCover(ctx, r.ID); err != nil {
			t.Fatal(err)
		}
		r.Info = &storage.ManagedCoverInfo{VolumeSerial: info.VolumeSerial, FileID: info.FileID, ByteSize: info.ByteSize, SHA256: info.SHA256}
		if err := source.ConfirmManagedCover(ctx, r.ID, *r.Info); err != nil {
			t.Fatal(err)
		}
		work, err := manga.NewWork(manga.NewWorkInput{URL: fmt.Sprintf("https://example.com/backup/%d", id), Title: "合成作品", ThumbnailPath: r.RelativePath}, time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := source.CreateWork(ctx, work); err != nil {
			t.Fatal(err)
		}
		return r
	}
	first, second, third := add(1), add(2), add(3)
	copyPath := filepath.Join(t.TempDir(), "copy.sqlite")
	if err := source.CopyDatabase(ctx, copyPath); err != nil {
		t.Fatal(err)
	}
	if err := storage.ValidateDatabaseCopy(ctx, copyPath); err != nil {
		t.Fatal(err)
	}
	add(4) // Source-only reference must not enter the copied snapshot.
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(directory, filepath.FromSlash(second.RelativePath))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, filepath.FromSlash(third.RelativePath)), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	var operations CoverOperations
	got, err := InspectBackupCopyCovers(ctx, copyPath, directory, &operations)
	if err != nil || len(got.Images) != 1 || got.Images[0].ID != first.ID || len(got.OmittedImages) != 2 || got.OmittedImages[0].Reason != "missing" || got.OmittedImages[1].Reason != "corrupt" {
		t.Fatal(got, err)
	}
	assertBackupReadOwnership(t, &operations, 0)
	if err := operations.CheckIdle(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(directory, directory+"-moved"); err != nil {
		t.Fatal("parent retained", err)
	}
	if err := os.Rename(directory+"-moved", directory); err != nil {
		t.Fatal(err)
	}
}
