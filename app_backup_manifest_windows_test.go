package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

func backupManifestApp(t *testing.T) *App {
	t.Helper()
	root := backupAppRoot(t)
	// Even an empty inventory requires an existing managed-image directory.
	if err := os.Mkdir(filepath.Join(root, "covers"), 0700); err != nil {
		t.Fatal(err)
	}
	return testApp(t, root)
}

func TestPrepareBackupManifest(t *testing.T) {
	a := backupManifestApp(t)
	if _, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/manifest", Title: "合成作品"}); err != nil {
		t.Fatal(err)
	}
	a.dataDirectory = func() (string, error) { t.Error("resolver called again"); return "", errors.New("unexpected resolver") }
	created := time.Date(2026, 11, 8, 22, 0, 0, 0, time.FixedZone("JST", 9*3600))
	r, err := a.prepareBackupManifest(context.Background(), created, "synthetic-app")
	if err != nil || !r.Database.Consumed || !r.Database.Cleanup.ReleaseConfirmed() {
		t.Fatal(r, err)
	}
	m, err := backup.DecodeManifest(bytes.NewReader(r.Manifest))
	if err != nil || m.CreatedAt != "2026-11-08T13:00:00Z" || m.AppVersion != "synthetic-app" || len(m.Migrations) != 9 || m.Migrations[8] != "009_tags.sql" || len(m.Database.SHA256) != 64 || m.Images == nil || m.OmittedImages == nil {
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

func TestBackupManifestAdmissionAndFailures(t *testing.T) {
	for _, mode := range []string{"success", "load-error", "sql-release", "invalid-metadata", "cancel", "late-cancel", "cleanup-warning", "cleanup-unconfirmed", "image-stopped"} {
		t.Run(mode, func(t *testing.T) {
			a := backupManifestApp(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cause := errors.New("synthetic manifest stage failure")
			var copyPath, digest string
			create := func(ctx context.Context, root string) (application.BackupDatabaseWork, error) {
				if root != a.dataDirectoryPath {
					t.Fatal("wrong root", root)
				}
				w, err := platform.CreateBackupDatabaseWork(ctx, root)
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
						r.Transition.DataClosed = false // Model unconfirmed release after real cleanup.
					}
					if mode == "cleanup-warning" || mode == "cleanup-unconfirmed" {
						err = errors.Join(err, cause)
					}
					return r, err
				}}, nil
			}
			called := false
			load := func(ctx context.Context, path string) ([]string, error) {
				called = true
				assertBackupImagesPaused(t, a)
				if path != copyPath {
					t.Fatal("not the owned copy", path)
				}
				b, err := os.ReadFile(path)
				if err != nil {
					t.Fatal("copy released before history/hash", err)
				}
				h := sha256.Sum256(b)
				digest = hex.EncodeToString(h[:])
				switch mode {
				case "load-error":
					return nil, cause
				case "sql-release":
					return nil, errors.Join(cause, storage.ErrDatabaseCopyRelease)
				case "invalid-metadata":
					return []string{"unknown.sql"}, nil
				case "cancel":
					cancel()
					return nil, ctx.Err()
				case "image-stopped":
					a.coverOperations.Stop()
				}
				return storage.DatabaseCopyMigrations(ctx, path)
			}
			r, err := a.runBackupManifest(ctx, time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC), "synthetic", create, load)
			if !called || !r.Database.Validated {
				t.Fatal("manifest stage not reached", r, err)
			}
			if mode == "success" {
				m, decodeErr := backup.DecodeManifest(bytes.NewReader(r.Manifest))
				if err != nil || decodeErr != nil || m.Database.SHA256 != digest || !r.Database.Consumed {
					t.Fatal(m, err, decodeErr)
				}
			} else if err == nil || r.Manifest != nil || r.Covers.Images != nil || r.Covers.OmittedImages != nil {
				t.Fatal("partial preparation escaped", r, err)
			}
			if mode == "load-error" || mode == "sql-release" || mode == "cleanup-warning" || mode == "cleanup-unconfirmed" {
				if !errors.Is(err, cause) {
					t.Fatal("cause lost", err)
				}
			}
			if (mode == "cancel" || mode == "late-cancel") && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
			end, admissionErr := a.operationGate.BeginNormal()
			if mode == "sql-release" || mode == "cleanup-unconfirmed" || mode == "image-stopped" {
				if end != nil || !errors.Is(admissionErr, application.ErrOperationsStopped) || r.FinishError == nil {
					t.Fatal("unsafe resume", r, admissionErr)
				}
			} else {
				if admissionErr != nil || r.FinishError != nil {
					t.Fatal("failed to resume confirmed release", r, admissionErr)
				}
				end()
			}
		})
	}
}
