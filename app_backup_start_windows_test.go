package main

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

func syntheticBackupSuccess(context.Context, application.BackupExecutionID) (backupSaveRunResult, bool, error) {
	return backupSaveRunResult{}, true, nil // No worker-owned resources acquired.
}

func TestAppBackupRunCancellationLifetime(t *testing.T) {
	a := testApp(t, t.TempDir())
	if _, err := a.LoadCoverWarnings(); err != nil {
		t.Fatal(err)
	}
	end, err := a.operationGate.BeginNormal()
	if err != nil {
		t.Fatal(err)
	}
	defer end()
	read, err := a.coverOperations.BeginRead()
	if err != nil {
		t.Fatal(err)
	}
	defer read.End()
	entered := make(chan struct{})
	release, finish := holdBackupRun(t)
	run, err := a.startBackupRun("synthetic.zip", func(ctx context.Context, _ application.BackupExecutionID) (backupSaveRunResult, bool, error) {
		close(entered)
		<-ctx.Done()
		<-release // Cleanup must finish even after cancellation.
		return backupSaveRunResult{}, true, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
		t.Fatal("worker passed active normal/image operations")
	default:
	}
	if err := read.End(); err != nil {
		t.Fatal(err)
	}
	end()
	waitBackupRun(t, entered)
	assertBackupImagesPaused(t, a)
	before := a.backupRuns.snapshot()
	if _, err := a.startBackupRun("duplicate.zip", syntheticBackupSuccess); !errors.Is(err, application.ErrBackupBusy) || !reflect.DeepEqual(before, a.backupRuns.snapshot()) {
		t.Fatal("duplicate changed active run", err)
	}
	if a.backupRuns.cancel(run.id) != application.BackupCancelAccepted {
		t.Fatal("cancel not accepted")
	}
	if got := a.backupRuns.snapshot(); got.result != nil || got.execution.State != application.BackupExecutionCancelling {
		t.Fatal(got)
	}
	if !a.beforeClose(context.Background()) {
		t.Fatal("close passed cancellation cleanup")
	}
	finish()
	waitBackupRun(t, run.done)
	if got := a.backupRuns.snapshot(); got.result == nil || !errors.Is(got.result.err, context.Canceled) || got.result.save.FinishError != nil {
		t.Fatal("cancel result or resume decision lost", got)
	}
	if a.beforeClose(context.Background()) {
		t.Fatal("finished run blocked close")
	}
	previous := a.backupRuns.snapshot()
	if _, err := a.startBackupRun("after-close.zip", syntheticBackupSuccess); !errors.Is(err, application.ErrOperationsClosing) || !reflect.DeepEqual(previous, a.backupRuns.snapshot()) {
		t.Fatal("closing start erased result", err)
	}
	a.shutdown(context.Background())
	if got := a.backupRuns.snapshot(); got.result != nil || got.destination != "" || got.execution.ID != 0 {
		t.Fatal("shutdown retained result", got)
	}
}

func TestAppBackupRunPreexistingStopKeepsResult(t *testing.T) {
	for _, kind := range []string{"database", "images", "image closing"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t, t.TempDir())
			run, err := a.startBackupRun("previous.zip", syntheticBackupSuccess)
			if err != nil {
				t.Fatal(err)
			}
			waitBackupRun(t, run.done)
			previous := a.backupRuns.snapshot()
			var want error
			switch kind {
			case "database":
				if err := a.store.Close(); err != nil {
					t.Fatal(err)
				}
				want = storage.ErrDBStopped
			case "images":
				a.coverOperations.Stop()
				want = application.ErrCoverStopped
			case "image closing":
				if _, err := a.coverOperations.RequestClose(); err != nil {
					t.Fatal(err)
				}
				want = application.ErrCoverClosing
			}
			if _, err := a.startBackupRun("rejected.zip", syntheticBackupSuccess); !errors.Is(err, want) || !reflect.DeepEqual(previous, a.backupRuns.snapshot()) {
				t.Fatal("preexisting stop erased previous result", err)
			}
			if a.beforeClose(context.Background()) {
				t.Fatal("rejected start leaked reservation")
			}
		})
	}
}

func TestAppBackupRunUnsafeOutcomes(t *testing.T) {
	for _, kind := range []string{"unknown", "future state", "release unverified"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t, t.TempDir())
			run, err := a.startBackupRun("preserve.zip", func(context.Context, application.BackupExecutionID) (backupSaveRunResult, bool, error) {
				var result backupSaveRunResult
				if kind == "unknown" {
					result.Save.Publication.State = platform.BackupPublishUnknown
				} else if kind == "future state" {
					result.Save.Publication.State = platform.BackupPublicationState(255)
				}
				return result, kind != "release unverified", nil
			})
			if err != nil {
				t.Fatal(err)
			}
			waitBackupRun(t, run.done)
			before := a.backupRuns.snapshot()
			var want error = errBackupRetryUnsafe
			if kind == "release unverified" {
				want = application.ErrOperationsStopped
				if before.result == nil || !errors.Is(before.result.err, errBackupReleaseUnverified) {
					t.Fatal("missing release warning", before)
				}
			} else {
				end, err := a.operationGate.BeginNormal()
				if err != nil {
					t.Fatal("unknown result stopped released normal work", err)
				}
				end()
			}
			if _, err := a.startBackupRun("retry.zip", syntheticBackupSuccess); !errors.Is(err, want) || !reflect.DeepEqual(before, a.backupRuns.snapshot()) {
				t.Fatal("unsafe retry changed previous result", err)
			}
			if a.beforeClose(context.Background()) {
				t.Fatal("preserved outcome prevented exit")
			}
		})
	}
}

func TestAppBackupRunCloseProtectsOutcomeStorage(t *testing.T) {
	a := testApp(t, t.TempDir())
	a.backupBusyNotice = func(context.Context) {}
	release, finish := holdBackupRun(t)
	// Model the gap after gate release and before result storage: no gate ticket
	// exists, but the run is still active. Check inside RequestClose's callback.
	run, err := a.backupRuns.start(a.ctx, "synthetic.zip", func(context.Context, application.BackupExecutionID) (backupSaveRunResult, error) {
		<-release
		return backupSaveRunResult{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !a.beforeClose(context.Background()) {
		t.Fatal("close passed unfinished result storage")
	}
	finish()
	waitBackupRun(t, run.done)
	if a.beforeClose(context.Background()) {
		t.Fatal("finished result blocked close")
	}
}

func TestBackupRunCloseDiscardsLateResult(t *testing.T) {
	var state backupRunState
	release, finish := holdBackupRun(t)
	run, err := state.start(context.Background(), "late.zip", func(ctx context.Context, _ application.BackupExecutionID) (backupSaveRunResult, error) {
		<-ctx.Done()
		<-release
		return backupSaveRunResult{}, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	state.close()
	if _, err := state.start(context.Background(), "new.zip", func(context.Context, application.BackupExecutionID) (backupSaveRunResult, error) {
		t.Error("closed worker started")
		return backupSaveRunResult{}, nil
	}); !errors.Is(err, errBackupRunsClosed) {
		t.Fatal(err)
	}
	finish()
	waitBackupRun(t, run.done)
	if got := state.snapshot(); got.result != nil || got.destination != "" || got.execution.ID != 0 {
		t.Fatal("late result revived closed owner", got)
	}
}

func TestAppBackupRunStartCloseRace(t *testing.T) {
	for range 20 {
		a := testApp(t, t.TempDir())
		a.backupBusyNotice = func(context.Context) {}
		if _, err := a.LoadCoverWarnings(); err != nil {
			t.Fatal(err)
		}
		release, finish := holdBackupRun(t)
		start := make(chan struct{})
		var run backupRun
		var err error
		var refused bool
		var wg sync.WaitGroup
		wg.Go(func() {
			<-start
			run, err = a.startBackupRun("race.zip", func(context.Context, application.BackupExecutionID) (backupSaveRunResult, bool, error) {
				<-release
				return backupSaveRunResult{}, true, nil
			})
		})
		wg.Go(func() { <-start; refused = a.beforeClose(context.Background()) })
		close(start)
		wg.Wait()
		if err == nil {
			if !refused {
				t.Fatal("both start and close accepted")
			}
			finish()
			waitBackupRun(t, run.done)
		} else if !errors.Is(err, application.ErrOperationsClosing) || refused {
			t.Fatal("inconsistent race decision", err, refused)
		}
		a.shutdown(context.Background())
	}
}
