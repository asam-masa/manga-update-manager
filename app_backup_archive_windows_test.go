package main

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

func backupArchiveApp(t *testing.T) (*App, string) {
	t.Helper()
	root := backupAppRoot(t)
	var body bytes.Buffer
	if err := png.Encode(&body, image.NewNRGBA(image.Rect(0, 0, 3, 4))); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source.png")
	if err := os.WriteFile(source, body.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	a := NewApp()
	a.dataDirectory = func() (string, error) { return root, nil }
	a.chooseCover = func(context.Context) (string, error) { return source, nil }
	a.startup(context.Background())
	t.Cleanup(func() { a.shutdown(context.Background()) })
	if _, err := a.LoadCoverWarnings(); err != nil {
		t.Fatal(err)
	}
	w, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/archive", Title: "合成ZIP作品"})
	if err != nil {
		t.Fatal(err)
	}
	if r, err := a.SelectWorkCover(w.ID); err != nil || r.Work == nil {
		t.Fatal(r, err)
	}
	a.dataDirectory = func() (string, error) { t.Error("resolver called again"); return "", errors.New("unexpected resolver") }
	return a, root
}

func TestAppWriteBackupArchive(t *testing.T) {
	a, _ := backupArchiveApp(t)
	var out bytes.Buffer
	r, err := a.writeBackupArchive(context.Background(), &out, time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC), "synthetic-app")
	if err != nil || !r.Database.Consumed || !r.Database.Cleanup.ReleaseConfirmed() {
		t.Fatal(r, err)
	}
	m, err := backup.ValidateArchive(bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil || len(m.Images) != 1 || len(m.Migrations) != 9 || m.AppVersion != "synthetic-app" {
		t.Fatal(m, err)
	}
	if _, err := os.Stat(r.Database.Cleanup.Directory.Path); !os.IsNotExist(err) {
		t.Fatal("work retained", err)
	}
	end, err := a.beginNormalOperation()
	if err != nil {
		t.Fatal(err)
	}
	end()
}

type appArchiveWriter func([]byte) (int, error)

func (w appArchiveWriter) Write(p []byte) (int, error) { return w(p) }

func TestAppBackupArchiveFailuresAndAdmission(t *testing.T) {
	for _, mode := range []string{"success", "load-error", "sql-release", "missing-image", "output-error", "output-release", "cancel", "late-cancel", "stop", "cleanup-warning", "cleanup-unconfirmed"} {
		t.Run(mode, func(t *testing.T) {
			a, root := backupArchiveApp(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cause := errors.New("synthetic ZIP stage failure")
			var copyPath string
			create := func(ctx context.Context, directory string) (application.BackupDatabaseWork, error) {
				w, err := platform.CreateBackupDatabaseWork(ctx, directory)
				if err != nil {
					return nil, err
				}
				copyPath = w.Path()
				return &delayedDatabaseWork{BackupDatabaseWork: w, cleanup: func() (platform.BackupDatabaseCleanupResult, error) {
					assertBackupImagesPaused(t, a)
					r, err := w.Cleanup()
					if mode == "late-cancel" {
						cancel()
					}
					if mode == "cleanup-unconfirmed" {
						r.Transition.DataClosed = false
					}
					if mode == "cleanup-warning" || mode == "cleanup-unconfirmed" {
						err = errors.Join(err, cause)
					}
					return r, err
				}}, nil
			}
			load := func(ctx context.Context, path string) ([]storage.ManagedCover, error) {
				assertBackupImagesPaused(t, a)
				if path != copyPath {
					t.Fatal("not the owned copy", path)
				}
				if mode == "load-error" {
					return nil, cause
				}
				if mode == "sql-release" {
					return nil, errors.Join(cause, storage.ErrDatabaseCopyRelease)
				}
				records, err := storage.DatabaseCopyCovers(ctx, path, backup.MaxImages)
				if err == nil && mode == "missing-image" {
					err = os.Remove(filepath.Join(root, filepath.FromSlash(records[0].RelativePath)))
				}
				if mode == "stop" {
					a.coverOperations.Stop()
				}
				return records, err
			}
			var out bytes.Buffer
			writer := appArchiveWriter(func(p []byte) (int, error) {
				assertBackupImagesPaused(t, a)
				if mode == "output-error" {
					return 0, cause
				}
				if mode == "output-release" {
					return 0, errors.Join(cause, platform.ErrCoverRelease)
				}
				if mode == "cancel" {
					cancel()
				}
				return out.Write(p)
			})
			r, err := a.runBackupArchive(ctx, writer, time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC), "synthetic", create, load)
			if mode == "success" {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := backup.ValidateArchive(bytes.NewReader(out.Bytes()), int64(out.Len())); err != nil {
					t.Fatal(err)
				}
			} else if err == nil || r.Manifest != nil || r.Covers.Images != nil || r.Covers.OmittedImages != nil {
				t.Fatal("partial result escaped", r, err)
			}
			if (mode == "load-error" || mode == "sql-release" || mode == "output-error" || mode == "output-release" || mode == "cleanup-warning" || mode == "cleanup-unconfirmed") && !errors.Is(err, cause) {
				t.Fatal("cause lost", err)
			}
			if (mode == "cancel" || mode == "late-cancel") && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
			end, admissionErr := a.operationGate.BeginNormal()
			if mode == "sql-release" || mode == "output-release" || mode == "stop" || mode == "cleanup-unconfirmed" {
				if end != nil || !errors.Is(admissionErr, application.ErrOperationsStopped) || r.FinishError == nil {
					t.Fatal("unsafe resume", r, admissionErr)
				}
			} else {
				if admissionErr != nil || r.FinishError != nil {
					t.Fatal("confirmed release did not resume", r, admissionErr)
				}
				end()
			}
		})
	}
}
