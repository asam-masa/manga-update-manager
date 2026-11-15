package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

type managedSaveProbe struct {
	*appSaveProbe
	onPublish func()
}

func TestStartBackupSaveUsesManagedLifetime(t *testing.T) {
	a, root := backupArchiveApp(t)
	final := filepath.Join(root, "real-managed.zip")
	run, err := a.startBackupSave(final, time.Now(), "managed-entry")
	if err != nil {
		t.Fatal(err)
	}
	waitBackupRun(t, run.done)
	view := a.backupRuns.snapshot()
	if view.result == nil || view.result.err != nil || view.result.save.Save.Publication.State != platform.BackupPublishConfirmed {
		t.Fatal(view)
	}
	f, err := os.Open(final)
	if err != nil {
		t.Fatal(err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	m, checkErr := backup.ValidateArchive(f, info.Size())
	closeErr := f.Close()
	if checkErr != nil || closeErr != nil || m.AppVersion != "managed-entry" || len(m.Images) != 1 {
		t.Fatal(m, checkErr, closeErr)
	}
}

func (p *managedSaveProbe) PublishNoReplace(ctx context.Context) (platform.BackupPublication, error) {
	if p.onPublish != nil {
		p.onPublish()
	}
	return p.appSaveProbe.PublishNoReplace(ctx)
}

func TestManagedBackupSaveOutcomes(t *testing.T) {
	for _, mode := range []string{"success", "cancel-first", "publish-first", "unknown", "close", "db-unconfirmed", "collision", "write", "db-cleanup"} {
		t.Run(mode, func(t *testing.T) {
			a, root := backupArchiveApp(t)
			final := filepath.Join(root, "managed.zip")
			if mode == "collision" {
				if err := os.WriteFile(final, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cause := errors.New("synthetic managed save failure")
			var probe *appSaveProbe
			dbCleaned := false
			run, err := a.startBackupRun(final, func(ctx context.Context, id application.BackupExecutionID) (backupSaveRunResult, bool, error) {
				return a.runReservedBackupSave(ctx, id, final, time.Now(), "managed-test",
					func(ctx context.Context, root string) (application.BackupDatabaseWork, error) {
						w, err := platform.CreateBackupDatabaseWork(ctx, root)
						if err != nil {
							return nil, err
						}
						return &delayedDatabaseWork{BackupDatabaseWork: w, cleanup: func() (platform.BackupDatabaseCleanupResult, error) {
							r, err := w.Cleanup()
							dbCleaned = true
							if mode == "cancel-first" && a.backupRuns.cancel(id) != application.BackupCancelAccepted {
								t.Error("cancel refused before publication")
							}
							if mode == "db-unconfirmed" {
								r.Transition.DataClosed = false
							}
							if mode == "db-cleanup" {
								err = errors.Join(err, cause)
							}
							return r, err
						}}, nil
					}, func(ctx context.Context, target string) (backupSaveFile, error) {
						f, err := platform.CreateBackupTemp(ctx, target)
						if err != nil {
							return nil, err
						}
						probe = &appSaveProbe{BackupTemp: f, a: a, t: t, mode: mode, cause: cause, dbCleaned: &dbCleaned}
						return &managedSaveProbe{appSaveProbe: probe, onPublish: func() {
							if mode == "publish-first" && a.backupRuns.cancel(id) != application.BackupCancelTooLate {
								t.Error("cancel accepted after publication gate")
							}
						}}, nil
					}, storage.DatabaseCopyCoverWorks)
			})
			if err != nil {
				t.Fatal(err)
			}
			waitBackupRun(t, run.done)
			view := a.backupRuns.snapshot()
			if view.execution.ID != run.id || view.result == nil || view.progress.Stage != backupStageCleanup {
				t.Fatal("wrong retained run", view)
			}
			r, resultErr := view.result.save, view.result.err
			confirmed := mode == "success" || mode == "publish-first" || mode == "close"
			state := platform.BackupPublishNotAttempted
			if confirmed {
				state = platform.BackupPublishConfirmed
			}
			if mode == "unknown" {
				state = platform.BackupPublishUnknown
			}
			if mode == "collision" {
				state = platform.BackupPublishCollision
			}
			if r.Save.Publication.State != state || probe == nil {
				t.Fatal(r, resultErr)
			}
			if (mode == "success" || mode == "publish-first") != (resultErr == nil) {
				t.Fatal("wrong error", mode, resultErr)
			}
			if mode == "cancel-first" && (!errors.Is(resultErr, context.Canceled) || probe.publishes != 0) {
				t.Fatal("accepted cancel published", probe.publishes, resultErr)
			}
			if mode == "unknown" && (probe.discards != 0 || probe.closes != 1) {
				t.Fatal("unknown was discarded/retried", probe)
			}
			if mode == "unknown" || mode == "close" || mode == "write" || mode == "db-cleanup" {
				if !errors.Is(resultErr, cause) {
					t.Fatal("cause lost", resultErr)
				}
			}
			if confirmed {
				f, err := os.Open(final)
				if err != nil {
					t.Fatal(err)
				}
				info, statErr := f.Stat()
				if statErr != nil {
					_ = f.Close()
					t.Fatal(statErr)
				}
				m, checkErr := backup.ValidateArchive(f, info.Size())
				closeErr := f.Close()
				if checkErr != nil || closeErr != nil || m.AppVersion != "managed-test" || len(m.Images) != 1 {
					t.Fatal(m, checkErr, closeErr)
				}
			} else if mode == "collision" {
				if b, err := os.ReadFile(final); err != nil || string(b) != "keep" {
					t.Fatal("collision overwritten", err)
				}
			} else if _, err := os.Stat(final); !os.IsNotExist(err) {
				t.Fatal("unexpected completion", err)
			}
			end, admissionErr := a.operationGate.BeginNormal()
			stopped := mode == "db-unconfirmed" || mode == "close"
			if stopped {
				if !errors.Is(admissionErr, application.ErrOperationsStopped) || !errors.Is(r.FinishError, errBackupReleaseUnverified) {
					t.Fatal(r, admissionErr)
				}
			} else {
				if admissionErr != nil {
					t.Fatal(admissionErr)
				}
				end()
			}
			if mode == "unknown" {
				if _, err := a.startBackupSave(final, time.Now(), "retry"); !errors.Is(err, errBackupRetryUnsafe) {
					t.Fatal("unknown retried", err)
				}
			}
		})
	}
}

type publicationCallProbe struct {
	backupSaveFile
	calls atomic.Int32
}

func (p *publicationCallProbe) Path() string { return "synthetic-temporary" }
func (p *publicationCallProbe) PublishNoReplace(context.Context) (platform.BackupPublication, error) {
	p.calls.Add(1)
	return platform.BackupPublication{State: platform.BackupPublishConfirmed}, nil
}

func TestManagedPublicationCancelRace(t *testing.T) {
	for range 30 {
		var owner backupRunState
		probe := &publicationCallProbe{}
		start := make(chan struct{})
		run, err := owner.start(context.Background(), "synthetic", func(ctx context.Context, id application.BackupExecutionID) (backupSaveRunResult, error) {
			<-start
			publication, err := (backupExecutionFile{backupSaveFile: probe, owner: &owner, id: id}).PublishNoReplace(ctx)
			return backupSaveRunResult{Save: application.BackupSaveResult{Publication: publication}}, err
		})
		if err != nil {
			t.Fatal(err)
		}
		cancelled := make(chan application.BackupCancelDecision, 1)
		go func() { <-start; cancelled <- owner.cancel(run.id) }()
		close(start)
		decision := <-cancelled
		waitBackupRun(t, run.done)
		if decision == application.BackupCancelAccepted {
			if probe.calls.Load() != 0 || !errors.Is(owner.snapshot().result.err, errBackupPublicationDenied) {
				t.Fatal("accepted cancel reached native publication")
			}
		} else if probe.calls.Load() != 1 || (decision != application.BackupCancelTooLate && decision != application.BackupCancelFinished) {
			t.Fatal("unexpected publication/cancel", decision, probe.calls.Load())
		}
	}
}
