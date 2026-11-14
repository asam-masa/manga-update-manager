package main

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

func TestAppBackupStageNotifications(t *testing.T) {
	for _, mode := range []string{"success", "create-work", "write", "cancel", "db-cleanup", "post-sync", "unknown", "delay-cancel"} {
		t.Run(mode, func(t *testing.T) {
			a, root := backupArchiveApp(t)
			var owner backupRunState // Synthetic owner; App worker wiring is #547.
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			cleanupEntered := make(chan struct{})
			release, finish := holdBackupRun(t)
			cause := errors.New("synthetic stage failure")
			dbCleaned := false
			createWork := func(ctx context.Context, root string) (application.BackupDatabaseWork, error) {
				if mode == "create-work" {
					return nil, cause
				}
				w, err := platform.CreateBackupDatabaseWork(ctx, root)
				if err != nil {
					return nil, err
				}
				return &delayedDatabaseWork{BackupDatabaseWork: w, cleanup: func() (platform.BackupDatabaseCleanupResult, error) {
					if mode == "delay-cancel" {
						close(cleanupEntered)
						<-release
					}
					r, err := w.Cleanup()
					dbCleaned = true
					if mode == "db-cleanup" {
						err = errors.Join(err, cause)
					}
					return r, err
				}}, nil
			}
			createOutput := func(ctx context.Context, target string) (backupSaveFile, error) {
				f, err := platform.CreateBackupTemp(ctx, target)
				if err != nil {
					return nil, err
				}
				return &appSaveProbe{BackupTemp: f, a: a, t: t, mode: mode, cause: cause, cancel: cancel, dbCleaned: &dbCleaned}, nil
			}
			var stages []backupStage // Only the synchronous worker writes this.
			run, err := owner.start(parent, "synthetic.zip", func(ctx context.Context, id application.BackupExecutionID) (backupSaveRunResult, error) {
				return a.runBackupSaveObserved(ctx, filepath.Join(root, "stages.zip"), time.Now(), "synthetic", createWork, createOutput, storage.DatabaseCopyCoverWorks,
					func(stage application.BackupStage) {
						if !owner.reportStage(id, stage) {
							t.Error("live notification rejected", stage)
						}
						// Reading progress here must not reenter a held owner lock.
						view := owner.snapshot()
						if view.execution.ID != id || view.progress.Stage != stage || view.result != nil {
							t.Error("notification not tied to execution", view)
						}
						stages = append(stages, stage)
					})
			})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "delay-cancel" {
				waitBackupRun(t, cleanupEntered)
				if owner.cancel(run.id) != application.BackupCancelAccepted {
					t.Fatal("cancel not accepted during DB cleanup")
				}
				if got := owner.snapshot(); got.progress.Stage != backupStageCleanup || got.result != nil || got.execution.CanCancel {
					t.Fatal("delayed cleanup became completion", got)
				}
				finish()
			}
			waitBackupRun(t, run.done)
			want := []backupStage{backupStageWaiting, backupStagePreparing}
			if mode != "create-work" {
				want = append(want, backupStageDatabase, backupStageDatabaseCheck, backupStageImages, backupStageMetadata, backupStageZIP)
				if mode != "write" && mode != "cancel" {
					want = append(want, backupStageDatabaseCheck)
				}
				want = append(want, backupStageCleanup)
				if mode == "success" || mode == "post-sync" || mode == "unknown" {
					want = append(want, backupStagePublishing)
				}
				want = append(want, backupStageCleanup)
			}
			view := owner.snapshot()
			if !reflect.DeepEqual(stages, want) || view.result == nil || view.progress.Stage != want[len(want)-1] {
				t.Fatal("stage order changed or unentered stage reported", stages, want, view)
			}
			if mode == "success" {
				if view.result.err != nil || view.result.save.Save.Publication.State != platform.BackupPublishConfirmed {
					t.Fatal(view.result)
				}
			} else if view.result.err == nil {
				t.Fatal("notification erased failure")
			}
			if (mode == "cancel" || mode == "delay-cancel") && !errors.Is(view.result.err, context.Canceled) {
				t.Fatal("cancellation lost", view.result.err)
			}
		})
	}
}
