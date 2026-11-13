package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/platform"
)

type appSaveProbe struct {
	*platform.BackupTemp
	a                           *App
	t                           *testing.T
	mode                        string
	cause                       error
	cancel                      context.CancelFunc
	dbCleaned                   *bool
	posted                      bool
	publishes, discards, closes int
}

func (p *appSaveProbe) Write(b []byte) (int, error) {
	assertBackupImagesPaused(p.t, p.a)
	if p.mode == "write" || p.mode == "discard-refused" || p.mode == "discard-check" {
		return 0, p.cause
	}
	return p.BackupTemp.Write(b)
}

func (p *appSaveProbe) Sync() error {
	assertBackupImagesPaused(p.t, p.a)
	if p.mode == "sync" || (p.mode == "post-sync" && p.posted) {
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

func (p *appSaveProbe) PublishNoReplace(ctx context.Context) (platform.BackupPublication, error) {
	assertBackupImagesPaused(p.t, p.a)
	if !*p.dbCleaned {
		p.t.Fatal("published before DB cleanup")
	}
	p.publishes++
	p.posted = true
	if p.mode == "unknown" {
		return platform.BackupPublication{State: platform.BackupPublishUnknown, TemporaryPath: p.Path()}, p.cause
	}
	r, err := p.BackupTemp.PublishNoReplace(ctx)
	if p.mode == "publish-cancel" {
		p.cancel()
	}
	return r, err
}

func (p *appSaveProbe) Close() error {
	assertBackupImagesPaused(p.t, p.a)
	p.closes++
	err := p.BackupTemp.Close()
	if p.mode == "close" {
		return errors.Join(err, platform.ErrBackupTempRelease, p.cause)
	}
	if p.mode == "parent-release" {
		return errors.Join(err, platform.ErrBackupDirectoryRelease, p.cause)
	}
	return err
}

func (p *appSaveProbe) Discard() (platform.BackupDiscardResult, error) {
	assertBackupImagesPaused(p.t, p.a)
	if !*p.dbCleaned {
		p.t.Fatal("discard before DB cleanup")
	}
	p.discards++
	if p.mode == "discard-refused" {
		return platform.BackupDiscardResult{TemporaryPath: p.Path()}, p.cause
	}
	r, err := p.BackupTemp.Discard()
	if p.mode == "discard-check" {
		r.Absent = false
		err = errors.Join(err, p.cause)
	}
	return r, err
}

func TestAppSaveBackupArchive(t *testing.T) {
	a, _ := backupArchiveApp(t)
	final := filepath.Join(backupAppRoot(t), "保存.zip")
	r, err := a.saveBackupArchive(context.Background(), final, time.Now(), "synthetic-save")
	if err != nil || r.Save.Publication.State != platform.BackupPublishConfirmed || !r.OutputReleaseConfirmed {
		t.Fatal(r, err)
	}
	f, err := os.Open(final)
	if err != nil {
		t.Fatal(err)
	}
	info, statErr := f.Stat()
	if statErr != nil {
		_ = f.Close()
		t.Fatal(statErr)
	}
	m, validationErr := backup.ValidateArchive(f, info.Size())
	closeErr := f.Close()
	if validationErr != nil || closeErr != nil || len(m.Images) != 1 || m.AppVersion != "synthetic-save" {
		t.Fatal(m, validationErr, closeErr)
	}
}

func TestAppBackupSaveOutcomesAndReservation(t *testing.T) {
	for _, mode := range []string{"success", "collision", "write", "sync", "db-cleanup", "db-unconfirmed", "db-create", "stop", "cancel", "late-cancel", "publish-cancel", "unknown", "post-sync", "close", "parent-release", "discard-refused", "discard-check", "create", "create-release"} {
		t.Run(mode, func(t *testing.T) {
			a, _ := backupArchiveApp(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cause := errors.New("synthetic save failure")
			final := filepath.Join(backupAppRoot(t), "backup.zip")
			if mode == "collision" {
				if err := os.WriteFile(final, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			dbCleaned := false
			createWork := func(ctx context.Context, root string) (application.BackupDatabaseWork, error) {
				if mode == "db-create" {
					return nil, cause // Unknown acquisition result must keep admission stopped.
				}
				w, err := platform.CreateBackupDatabaseWork(ctx, root)
				if err != nil {
					return nil, err
				}
				return &delayedDatabaseWork{BackupDatabaseWork: w, cleanup: func() (platform.BackupDatabaseCleanupResult, error) {
					assertBackupImagesPaused(t, a)
					r, err := w.Cleanup()
					dbCleaned = true
					if mode == "db-cleanup" {
						err = errors.Join(err, cause)
					}
					if mode == "db-unconfirmed" {
						r.Transition.DataClosed = false
					}
					if mode == "late-cancel" {
						cancel()
					}
					return r, err
				}}, nil
			}
			var probe *appSaveProbe
			createOutput := func(ctx context.Context, target string) (backupSaveFile, error) {
				assertBackupImagesPaused(t, a)
				if mode == "db-create" {
					t.Fatal("output created after DB failure")
				}
				if mode == "create" {
					return (*platform.BackupTemp)(nil), cause
				}
				if mode == "create-release" {
					return nil, errors.Join(cause, platform.ErrBackupDirectoryRelease)
				}
				f, err := platform.CreateBackupTemp(ctx, target)
				if err != nil {
					return nil, err
				}
				probe = &appSaveProbe{BackupTemp: f, a: a, t: t, mode: mode, cause: cause, cancel: cancel, dbCleaned: &dbCleaned}
				return probe, nil
			}
			r, err := a.runBackupSave(ctx, final, time.Now(), "synthetic", createWork, createOutput)
			confirmed := mode == "success" || mode == "post-sync" || mode == "close" || mode == "parent-release" || mode == "publish-cancel"
			state := platform.BackupPublishNotAttempted
			if confirmed {
				state = platform.BackupPublishConfirmed
			}
			if mode == "collision" {
				state = platform.BackupPublishCollision
			}
			if mode == "unknown" {
				state = platform.BackupPublishUnknown
			}
			if r.Save.Publication.State != state {
				t.Fatal("wrong publication", r, err)
			}
			if mode == "success" {
				if err != nil || len(r.Manifest) == 0 {
					t.Fatal(r, err)
				}
			} else if err == nil || r.Manifest != nil || r.Covers.Images != nil || r.Covers.OmittedImages != nil {
				t.Fatal("failure or partial result lost", r, err)
			}
			if mode != "success" && mode != "collision" && mode != "cancel" && mode != "late-cancel" && mode != "publish-cancel" && mode != "db-unconfirmed" && mode != "stop" && !errors.Is(err, cause) {
				t.Fatal("cause lost", err)
			}
			if (mode == "cancel" || mode == "late-cancel" || mode == "publish-cancel") && !errors.Is(err, context.Canceled) {
				t.Fatal("cancel lost", err)
			}
			if (mode == "create" || mode == "create-release") && !errors.Is(r.OutputCreationError, cause) {
				t.Fatal("creation observation lost", r)
			}
			if confirmed {
				if _, e := os.Stat(final); e != nil || r.Save.Cleanup != nil {
					t.Fatal("confirmed file discarded", r, e)
				}
			} else if mode == "collision" {
				if b, e := os.ReadFile(final); e != nil || string(b) != "keep" {
					t.Fatal("collision modified", e)
				}
			} else if _, e := os.Stat(final); !os.IsNotExist(e) {
				t.Fatal("unexpected publication", e)
			}
			if probe != nil {
				wantPublish, wantDiscard, wantClose := 0, 1, 0
				if confirmed || mode == "collision" || mode == "unknown" {
					wantPublish = 1
				}
				if confirmed || mode == "unknown" {
					wantDiscard, wantClose = 0, 1
				}
				if mode == "discard-refused" {
					wantClose = 1
				}
				if probe.publishes != wantPublish || probe.discards != wantDiscard || probe.closes != wantClose {
					t.Fatal("repeated or missing lifecycle", probe.publishes, probe.discards, probe.closes)
				}
				if _, e := os.Stat(probe.Path()); (mode == "unknown" || mode == "discard-refused") != (e == nil) {
					t.Fatal("wrong preservation", mode, e)
				}
			}
			end, admissionErr := a.operationGate.BeginNormal()
			stopped := mode == "db-unconfirmed" || mode == "db-create" || mode == "stop" || mode == "close" || mode == "parent-release" || mode == "create-release"
			if stopped {
				if end != nil || !errors.Is(admissionErr, application.ErrOperationsStopped) || r.FinishError == nil {
					t.Fatal("unsafe resume", r, admissionErr)
				}
			} else {
				if admissionErr != nil {
					t.Fatal("confirmed release did not resume", admissionErr)
				}
				end()
			}
			if r.OutputReleaseConfirmed == (mode == "close" || mode == "parent-release" || mode == "create-release") {
				t.Fatal("wrong output release", r, err)
			}
		})
	}
}
