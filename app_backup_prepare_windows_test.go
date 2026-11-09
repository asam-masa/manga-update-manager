package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/platform"
)

type appPrepareProbe struct {
	*platform.BackupTemp
	a        *App
	t        *testing.T
	mode     string
	cause    error
	cancel   context.CancelFunc
	verified int
	synced   bool
	read     bool
}

func (p *appPrepareProbe) VerifyIdentity() error {
	assertBackupImagesPaused(p.t, p.a)
	p.verified++
	if p.mode == "identity" && p.verified == 2 {
		return p.cause
	}
	return p.BackupTemp.VerifyIdentity()
}

func (p *appPrepareProbe) Sync() error {
	assertBackupImagesPaused(p.t, p.a)
	p.synced = true
	if p.mode == "sync" {
		return p.cause
	}
	if p.mode == "cancel" {
		p.cancel()
	}
	if p.mode == "stop" {
		p.a.coverOperations.Stop()
	}
	return p.BackupTemp.Sync()
}

func (p *appPrepareProbe) ReadAt(b []byte, offset int64) (int, error) {
	assertBackupImagesPaused(p.t, p.a)
	p.read = true
	if !p.synced {
		p.t.Fatal("read before Sync")
	}
	if p.mode == "read" {
		return 0, p.cause
	}
	return p.BackupTemp.ReadAt(b, offset)
}

func TestAppPrepareBackupArchiveOnHeldFile(t *testing.T) {
	a, _ := backupArchiveApp(t)
	final := filepath.Join(backupAppRoot(t), "保存.zip")
	f, err := platform.CreateBackupTemp(context.Background(), final)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = f.Discard(); _ = f.Close() })
	r, err := a.prepareBackupArchive(context.Background(), f, time.Now(), "synthetic")
	if err != nil || len(r.Manifest) == 0 || len(r.Covers.Images) != 1 || !r.Database.Consumed {
		t.Fatal(r, err)
	}
	if err := f.VerifyIdentity(); err != nil {
		t.Fatal("file ownership lost", err)
	}
	if _, err := os.Stat(final); !os.IsNotExist(err) {
		t.Fatal("published before final save", err)
	}
	if _, err := os.Stat(r.Database.Cleanup.Directory.Path); !os.IsNotExist(err) {
		t.Fatal("DB work retained", err)
	}
}

func TestAppBackupPrepareFailuresAndReservation(t *testing.T) {
	for _, mode := range []string{"success", "nonempty", "sync", "read", "identity", "cancel", "stop", "cleanup", "late-cancel"} {
		t.Run(mode, func(t *testing.T) {
			a, _ := backupArchiveApp(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			final := filepath.Join(backupAppRoot(t), "backup.zip")
			f, err := platform.CreateBackupTemp(ctx, final)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = f.Discard(); _ = f.Close() })
			cause := errors.New("synthetic retained-file failure")
			probe := &appPrepareProbe{BackupTemp: f, a: a, t: t, mode: mode, cause: cause, cancel: cancel}
			if mode == "nonempty" {
				if _, err := f.Write([]byte("keep")); err != nil {
					t.Fatal(err)
				}
			}
			create := func(ctx context.Context, root string) (application.BackupDatabaseWork, error) {
				w, err := platform.CreateBackupDatabaseWork(ctx, root)
				if err != nil {
					return nil, err
				}
				return &delayedDatabaseWork{BackupDatabaseWork: w, cleanup: func() (platform.BackupDatabaseCleanupResult, error) {
					assertBackupImagesPaused(t, a)
					r, err := w.Cleanup()
					if mode == "cleanup" {
						err = errors.Join(err, cause)
					}
					if mode == "late-cancel" {
						cancel()
					}
					return r, err
				}}, nil
			}
			r, err := a.runBackupPrepare(ctx, probe, time.Now(), "synthetic", create)
			if mode == "success" {
				if err != nil || len(r.Manifest) == 0 || probe.verified != 2 || !probe.read {
					t.Fatal(r, err, probe.verified, probe.read)
				}
			} else if err == nil || r.Manifest != nil || r.Covers.Images != nil || r.Covers.OmittedImages != nil {
				t.Fatal("partial preparation escaped", r, err)
			}
			if (mode == "sync" || mode == "read" || mode == "identity" || mode == "cleanup") && !errors.Is(err, cause) {
				t.Fatal("cause lost", err)
			}
			if (mode == "cancel" || mode == "late-cancel") && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
			if mode == "nonempty" {
				var b [4]byte
				if _, e := f.ReadAt(b[:], 0); e != nil || string(b[:]) != "keep" || probe.synced {
					t.Fatal("existing content changed", b, e)
				}
			}
			if (mode == "sync" || mode == "cancel") && probe.read {
				t.Fatal("continued after Sync failure/cancellation")
			}
			if e := f.VerifyIdentity(); e != nil {
				t.Fatal("borrowed file closed", e)
			}
			if _, e := os.Stat(final); !os.IsNotExist(e) {
				t.Fatal("unexpected publication", e)
			}
			end, admissionErr := a.operationGate.BeginNormal()
			if mode == "stop" {
				if end != nil || !errors.Is(admissionErr, application.ErrOperationsStopped) {
					t.Fatal("unsafe resume", admissionErr)
				}
			} else {
				if admissionErr != nil {
					t.Fatal("confirmed release did not resume", admissionErr)
				}
				end()
			}
		})
	}
}
