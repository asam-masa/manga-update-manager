package main

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/platform"
)

func waitBackupRun(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("backup worker did not finish")
	}
}

func holdBackupRun(t *testing.T) (<-chan struct{}, func()) {
	t.Helper()
	ch := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(ch) }) }
	t.Cleanup(release)
	return ch, release
}

func TestBackupRunRetainsOutcomes(t *testing.T) {
	for _, state := range []platform.BackupPublicationState{platform.BackupPublishNotAttempted, platform.BackupPublishCollision, platform.BackupPublishConfirmed, platform.BackupPublishUnknown} {
		t.Run(string(rune('0'+state)), func(t *testing.T) {
			var s backupRunState
			if view := s.snapshot(); view.result != nil || view.execution.ID != 0 {
				t.Fatal(view)
			}
			cause := errors.New("synthetic operation or postprocessing failure")
			original := backupSaveRunResult{OutputReleaseConfirmed: state != platform.BackupPublishUnknown}
			original.Save.Publication = platform.BackupPublication{State: state, TemporaryPath: "synthetic-temp", FinalPath: "synthetic-final"}
			original.Save.Cleanup = &platform.BackupDiscardResult{TemporaryPath: "synthetic-temp", MarkedForDeletion: true, Absent: false}
			original.Warnings = []CoverWarningDTO{{Code: "synthetic", Message: "retained"}}
			original.Manifest = []byte("synthetic-metadata")
			original.Covers.Images = []backup.Image{{ID: "image", Path: "synthetic-image"}}
			original.Covers.OmittedImages = []backup.OmittedImage{{ID: "omitted", Reason: "missing"}}
			original.Database.OperationError, original.Database.CleanupError = cause, cause
			original.CreationError, original.FinishError, original.OutputCreationError = cause, cause, cause
			run, err := s.start(context.Background(), "chosen.zip", func(context.Context, application.BackupExecutionID) (backupSaveRunResult, error) {
				return original, cause
			})
			if err != nil {
				t.Fatal(err)
			}
			waitBackupRun(t, run.done)
			view := s.snapshot()
			if view.execution.State != application.BackupExecutionFinished || view.execution.ID != run.id || view.destination != "chosen.zip" || view.result == nil || !errors.Is(view.result.err, cause) || !reflect.DeepEqual(view.result.save, original) {
				t.Fatal("outcome was lost or rewritten", view)
			}
			// Returned metadata cannot rewrite the retained result.
			view.result.save.Manifest[0] = 'X'
			view.result.save.Warnings[0].Message = "changed"
			view.result.save.Covers.Images[0].Path = "changed"
			view.result.save.Covers.OmittedImages[0].Reason = "changed"
			view.result.save.Save.Cleanup.Absent = true
			if !reflect.DeepEqual(s.snapshot().result.save, original) {
				t.Fatal("snapshot aliased retained metadata")
			}
			// The worker's returned slices/pointer were also detached before done.
			original.Manifest[0], original.Warnings[0].Message = 'Y', "changed"
			original.Covers.Images[0].Path, original.Covers.OmittedImages[0].Reason = "changed", "changed"
			original.Save.Cleanup.Absent = true
			if got := s.snapshot().result.save; string(got.Manifest) != "synthetic-metadata" || got.Warnings[0].Message != "retained" || got.Covers.Images[0].Path != "synthetic-image" || got.Covers.OmittedImages[0].Reason != "missing" || got.Save.Cleanup.Absent {
				t.Fatal("worker metadata aliased retained result")
			}
		})
	}
}

func TestBackupRunRejectedStartKeepsResult(t *testing.T) {
	var s backupRunState
	completed, err := s.start(context.Background(), "previous.zip", func(context.Context, application.BackupExecutionID) (backupSaveRunResult, error) {
		return backupSaveRunResult{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	waitBackupRun(t, completed.done)
	before := s.snapshot()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.start(ctx, "rejected.zip", func(context.Context, application.BackupExecutionID) (backupSaveRunResult, error) {
		t.Error("rejected worker ran")
		return backupSaveRunResult{}, nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.start(context.Background(), "rejected.zip", nil); !errors.Is(err, errBackupWorkerRequired) || !reflect.DeepEqual(before, s.snapshot()) {
		t.Fatal("rejected begin changed previous result", err)
	}
	release, finish := holdBackupRun(t)
	next, err := s.start(context.Background(), "next.zip", func(context.Context, application.BackupExecutionID) (backupSaveRunResult, error) {
		<-release
		return backupSaveRunResult{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.snapshot(); got.execution.ID == completed.id || got.execution.ID != next.id || got.result != nil || got.destination != "next.zip" {
		t.Fatal("accepted start did not replace previous result", got)
	}
	if s.cancel(completed.id) != application.BackupCancelStale || s.beginPublication(completed.id) {
		t.Fatal("old request affected next run")
	}
	s.finish(completed.id, backupSaveRunResult{}, errors.New("late outcome"))
	if got := s.snapshot(); got.result != nil || got.execution.ID != next.id {
		t.Fatal("old completion replaced current run", got)
	}
	finish()
	waitBackupRun(t, next.done)
}

func TestBackupRunControlsDuringCleanup(t *testing.T) {
	for _, parentCancel := range []bool{false, true} {
		var s backupRunState
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		release, finish := holdBackupRun(t)
		entered := make(chan struct{})
		run, err := s.start(ctx, "synthetic.zip", func(ctx context.Context, id application.BackupExecutionID) (backupSaveRunResult, error) {
			close(entered)
			<-ctx.Done()
			<-release // Synthetic slow, non-cancellable cleanup.
			return backupSaveRunResult{}, ctx.Err()
		})
		if err != nil {
			t.Fatal(err)
		}
		waitBackupRun(t, entered)
		if parentCancel {
			cancel()
		} else if s.cancel(run.id) != application.BackupCancelAccepted {
			t.Fatal("cancel not accepted")
		}
		var wg sync.WaitGroup
		for range 12 {
			wg.Go(func() {
				view := s.snapshot()
				if view.result != nil || view.execution.State == application.BackupExecutionFinished || view.execution.CanCancel {
					t.Error("cleanup treated as complete", view)
				}
			})
		}
		wg.Wait()
		if _, err := s.start(context.Background(), "duplicate.zip", func(context.Context, application.BackupExecutionID) (backupSaveRunResult, error) {
			t.Error("duplicate worker ran")
			return backupSaveRunResult{}, nil
		}); !errors.Is(err, application.ErrBackupBusy) {
			t.Fatal(err)
		}
		select {
		case <-run.done:
			t.Fatal("completed before cleanup")
		default:
		}
		if s.beginPublication(run.id) {
			t.Fatal("publication after cancel")
		}
		finish()
		waitBackupRun(t, run.done)
		if got := s.snapshot(); got.result == nil || !errors.Is(got.result.err, context.Canceled) || got.execution.State != application.BackupExecutionFinished {
			t.Fatal("cancelled result missing", got)
		}
	}
}

func TestBackupRunKeepsConfirmedResultAfterLateCancel(t *testing.T) {
	var s backupRunState
	entered := make(chan struct{})
	release, finish := holdBackupRun(t)
	run, err := s.start(context.Background(), "confirmed.zip", func(ctx context.Context, id application.BackupExecutionID) (backupSaveRunResult, error) {
		if !s.beginPublication(id) {
			t.Error("publication not admitted")
		}
		close(entered)
		<-release
		result := backupSaveRunResult{OutputReleaseConfirmed: true}
		result.Save.Publication = platform.BackupPublication{State: platform.BackupPublishConfirmed, FinalPath: "confirmed.zip"}
		return result, application.ErrBackupPostprocess
	})
	if err != nil {
		t.Fatal(err)
	}
	waitBackupRun(t, entered)
	if s.cancel(run.id) != application.BackupCancelTooLate || s.snapshot().result != nil {
		t.Fatal("late cancel accepted or result prematurely finalized")
	}
	finish()
	waitBackupRun(t, run.done)
	got := s.snapshot()
	if got.result == nil || got.result.save.Save.Publication.State != platform.BackupPublishConfirmed || !errors.Is(got.result.err, application.ErrBackupPostprocess) {
		t.Fatal("confirmed result or warning lost", got)
	}
}
