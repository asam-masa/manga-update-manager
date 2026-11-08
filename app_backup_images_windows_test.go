package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

func TestBackupImagesUsesManagedCopy(t *testing.T) {
	ctx := context.Background()
	root := backupAppRoot(t)
	a := testApp(t, root)
	if _, err := a.LoadCoverWarnings(); err != nil {
		t.Fatal(err)
	}
	a.dataDirectory = func() (string, error) { t.Error("resolver called again"); return "", errors.New("unexpected resolver") }
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 3, 4))); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		id := fmt.Sprintf("%032x", i)
		info, err := platform.WriteNewCover(ctx, root, id, data.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		if err := a.store.ReserveManagedCover(ctx, id); err != nil {
			t.Fatal(err)
		}
		if err := a.store.ConfirmManagedCover(ctx, id, storage.ManagedCoverInfo{VolumeSerial: info.VolumeSerial, FileID: info.FileID, ByteSize: info.ByteSize, SHA256: info.SHA256}); err != nil {
			t.Fatal(err)
		}
		w, err := manga.NewWork(manga.NewWorkInput{URL: fmt.Sprintf("https://example.com/backup/%d", i), Title: "合成作品", ThumbnailPath: "covers/" + id + ".png"}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.store.CreateWork(ctx, w); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(filepath.Join(root, "covers", fmt.Sprintf("%032x.png", 2))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "covers", fmt.Sprintf("%032x.png", 3)), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := a.inspectBackupImages(ctx)
	if err != nil || !r.Database.Consumed || !r.Database.Cleanup.ReleaseConfirmed() || len(r.Covers.Images) != 1 || len(r.Covers.OmittedImages) != 2 {
		t.Fatal(r, err)
	}
	if r.Covers.Images[0].ID != fmt.Sprintf("%032x", 1) || r.Covers.OmittedImages[0].Reason != "missing" || r.Covers.OmittedImages[1].Reason != "corrupt" {
		t.Fatal(r.Covers)
	}
	if _, err := os.Stat(r.Database.Cleanup.Directory.Path); !os.IsNotExist(err) {
		t.Fatal("work directory retained", err)
	}
	end, err := a.beginNormalOperation()
	if err != nil {
		t.Fatal("normal admission not resumed", err)
	}
	end()
}

func TestBackupImagesFailureAndCleanup(t *testing.T) {
	for _, mode := range []string{"success", "inspect-error", "sql-release", "image-release", "image-stopped", "cancel", "cleanup-warning", "cleanup-unconfirmed", "late-cancel"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t, backupAppRoot(t))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cause := errors.New("synthetic failure")
			var path string
			create := func(ctx context.Context, root string) (application.BackupDatabaseWork, error) {
				w, err := platform.CreateBackupDatabaseWork(ctx, root)
				if err != nil {
					return nil, err
				}
				path = w.Path()
				return &delayedDatabaseWork{BackupDatabaseWork: w, cleanup: func() (platform.BackupDatabaseCleanupResult, error) {
					assertBackupImagesPaused(t, a)
					r, err := w.Cleanup()
					if mode == "late-cancel" {
						cancel()
					}
					if mode == "cleanup-unconfirmed" {
						r.Transition.DataClosed = false // Model unconfirmed release after real cleanup.
					}
					if mode == "cleanup-warning" || mode == "cleanup-unconfirmed" {
						err = errors.Join(err, cause)
					}
					return r, err
				}}, nil
			}
			called := false
			inspect := func(ctx context.Context, copyPath, root string, ops *application.CoverOperations) (application.BackupCoverInventory, error) {
				called = true
				assertBackupImagesPaused(t, a)
				if copyPath != path || root != a.dataDirectoryPath || ops != &a.coverOperations {
					t.Fatal("wrong pinned source", copyPath, root)
				}
				if err := storage.ValidateDatabaseCopy(ctx, copyPath); err != nil {
					t.Fatal("copy not readable during inspection", err)
				}
				var err error
				switch mode {
				case "inspect-error":
					err = cause
				case "sql-release":
					err = errors.Join(cause, storage.ErrDatabaseCopyRelease)
				case "image-release":
					err = errors.Join(cause, platform.ErrCoverRelease)
					ops.ObserveError(err) // Same notification contract as the real inspector.
				case "image-stopped":
					ops.Stop() // Successful observation followed by a stopped source.
				case "cancel":
					cancel()
					err = ctx.Err()
				}
				return application.BackupCoverInventory{Images: []backup.Image{{ID: "synthetic"}}}, err
			}
			r, err := a.runBackupImages(ctx, create, inspect)
			if !called || !r.Database.Copied || !r.Database.Validated {
				t.Fatal("inspection not reached", r, err)
			}
			if mode == "success" {
				if err != nil || len(r.Covers.Images) != 1 {
					t.Fatal(r, err)
				}
			} else if err == nil || r.Covers.Images != nil || r.Covers.OmittedImages != nil {
				t.Fatal("partial inventory escaped", r, err)
			}
			if mode == "cancel" || mode == "late-cancel" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation lost", err)
				}
			} else if mode != "success" && mode != "image-stopped" && !errors.Is(err, cause) {
				t.Fatal("original cause lost", err)
			}
			end, admissionErr := a.operationGate.BeginNormal()
			if mode == "sql-release" || mode == "image-release" || mode == "image-stopped" || mode == "cleanup-unconfirmed" {
				if end != nil || !errors.Is(admissionErr, application.ErrOperationsStopped) || r.FinishError == nil {
					t.Fatal("unsafe admission resumed", r, admissionErr)
				}
			} else {
				if admissionErr != nil || r.FinishError != nil {
					t.Fatal("confirmed release did not resume", r, admissionErr)
				}
				end()
			}
			if a.beforeClose(context.Background()) {
				t.Fatal("finished operation blocked exit")
			}
		})
	}
}

func TestBackupImagesDoesNotInspectAfterEarlyFailure(t *testing.T) {
	for _, earlyCancel := range []bool{false, true} {
		t.Run(fmt.Sprint(earlyCancel), func(t *testing.T) {
			a := testApp(t, backupAppRoot(t))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if earlyCancel {
				cancel()
			}
			cause := errors.New("creation failed")
			created := false
			r, err := a.runBackupImages(ctx, func(context.Context, string) (application.BackupDatabaseWork, error) {
				created = true
				return nil, cause
			}, func(context.Context, string, string, *application.CoverOperations) (application.BackupCoverInventory, error) {
				t.Error("inspection after early failure")
				return application.BackupCoverInventory{}, nil
			})
			want := cause
			if earlyCancel {
				want = context.Canceled
			}
			if created == earlyCancel || !errors.Is(err, want) || r.Covers.Images != nil || r.Database.Consumed {
				t.Fatal(r, err, created)
			}
		})
	}
}

func assertBackupImagesPaused(t *testing.T, a *App) {
	t.Helper()
	if end, err := a.operationGate.BeginNormal(); end != nil || !errors.Is(err, application.ErrOperationsPaused) {
		if end != nil {
			end()
		}
		t.Fatal("normal operation admitted", err)
	}
	if op, err := a.operationGate.BeginBackup(context.Background()); op != nil || !errors.Is(err, application.ErrBackupBusy) {
		if op != nil {
			op.End()
		}
		t.Fatal("second backup admitted", err)
	}
	a.backupBusyNotice = func(context.Context) {}
	if !a.beforeClose(context.Background()) {
		t.Fatal("exit allowed during image inspection/DB cleanup")
	}
}
