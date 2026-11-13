package application

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestBackupExecutionLifecycle(t *testing.T) {
	var e BackupExecutions
	if got := e.Snapshot(); got.ID != 0 || got.State != BackupExecutionIdle || got.CanCancel {
		t.Fatal(got)
	}
	if e.Cancel(0) != BackupCancelStale || e.Finish(0) || e.BeginPublication(0) {
		t.Fatal("zero identifier accepted")
	}
	id, ctx, err := e.Begin(context.Background())
	if err != nil || id == 0 {
		t.Fatal(id, err)
	}
	before := e.Snapshot()
	if before.State != BackupExecutionRunning || !before.CanCancel || ctx.Err() != nil {
		t.Fatal(before, ctx.Err())
	}
	if _, _, err := e.Begin(context.Background()); !errors.Is(err, ErrBackupBusy) || e.Snapshot() != before {
		t.Fatal("duplicate changed execution", err)
	}
	if e.Cancel(id) != BackupCancelAccepted || ctx.Err() != context.Canceled {
		t.Fatal("cancel not accepted")
	}
	if e.Cancel(id) != BackupCancelRepeated || e.BeginPublication(id) {
		t.Fatal("cancel allowed publication")
	}
	if got := e.Snapshot(); got.State != BackupExecutionCancelling || got.CanCancel {
		t.Fatal(got)
	}
	if _, _, err := e.Begin(context.Background()); !errors.Is(err, ErrBackupBusy) {
		t.Fatal("cancel was treated as completion", err)
	}
	if !e.Finish(id) || e.Finish(id) || e.Cancel(id) != BackupCancelFinished {
		t.Fatal("finish identity decision")
	}
	finished := e.Snapshot()
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := e.Begin(parent); !errors.Is(err, context.Canceled) || e.Snapshot() != finished {
		t.Fatal("rejected begin erased finished state", err)
	}
	next, nextCtx, err := e.Begin(context.Background())
	if err != nil || next == id || next == 0 {
		t.Fatal(next, err)
	}
	if e.Cancel(id) != BackupCancelStale || e.BeginPublication(id) || e.Finish(id) || nextCtx.Err() != nil {
		t.Fatal("old operation affected next execution")
	}
	if !e.BeginPublication(next) || e.BeginPublication(next) || e.Cancel(next) != BackupCancelTooLate || nextCtx.Err() != nil {
		t.Fatal("late cancel or repeated publication accepted")
	}
	if _, _, err := e.Begin(context.Background()); !errors.Is(err, ErrBackupBusy) {
		t.Fatal("publication was treated as completion", err)
	}
	if !e.Finish(next) || nextCtx.Err() != context.Canceled {
		t.Fatal("finish did not release context")
	}
}

func TestBackupExecutionParentCancellationAndIDExhaustion(t *testing.T) {
	var e BackupExecutions
	parent, cancel := context.WithCancel(context.Background())
	id, ctx, err := e.Begin(parent)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if ctx.Err() != context.Canceled || e.Snapshot().CanCancel || e.BeginPublication(id) {
		t.Fatal("cancelled application allowed publication")
	}
	if _, _, err := e.Begin(context.Background()); !errors.Is(err, ErrBackupBusy) {
		t.Fatal("parent cancellation released execution", err)
	}
	e.Finish(id)
	e.id = ^BackupExecutionID(0) // Arrange terminal identifier without billions of runs.
	before := e.Snapshot()
	if _, _, err := e.Begin(context.Background()); !errors.Is(err, ErrBackupExecutionIDExhausted) || e.Snapshot() != before {
		t.Fatal("identifier wraparound changed state", err)
	}
}

func TestBackupExecutionConcurrentBegins(t *testing.T) {
	var e BackupExecutions
	start := make(chan struct{})
	ids := make(chan BackupExecutionID, 16)
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			<-start
			id, _, err := e.Begin(context.Background())
			if err == nil {
				ids <- id
			} else if !errors.Is(err, ErrBackupBusy) {
				t.Error(err)
			}
		})
	}
	close(start)
	wg.Wait()
	close(ids)
	if len(ids) != 1 {
		t.Fatal("accepted starts", len(ids))
	}
	e.Finish(<-ids)
}

func TestBackupExecutionCancelPublicationRace(t *testing.T) {
	for range 200 {
		var e BackupExecutions
		id, ctx, err := e.Begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		var decision BackupCancelDecision
		var publishing bool
		var wg sync.WaitGroup
		wg.Go(func() { <-start; decision = e.Cancel(id) })
		wg.Go(func() { <-start; publishing = e.BeginPublication(id) })
		close(start)
		wg.Wait()
		switch decision {
		case BackupCancelAccepted:
			if publishing || ctx.Err() != context.Canceled {
				t.Fatal("cancel accepted but publication began", publishing, ctx.Err())
			}
		case BackupCancelTooLate:
			if !publishing || ctx.Err() != nil {
				t.Fatal("publication won but was cancelled", publishing, ctx.Err())
			}
		default:
			t.Fatal(decision)
		}
		if _, _, err := e.Begin(context.Background()); !errors.Is(err, ErrBackupBusy) {
			t.Fatal("new run before cleanup finished", err)
		}
		e.Finish(id)
	}
}

func TestBackupExecutionControlDuringWorkerWait(t *testing.T) {
	var e BackupExecutions
	id, ctx, err := e.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// A synthetic blocked I/O holds no control lock, even after cancellation.
	release := make(chan struct{})
	done := make(chan struct{})
	go func() { <-release; e.Finish(id); close(done) }()
	if e.Snapshot().ID != id || e.Cancel(id) != BackupCancelAccepted || ctx.Err() != context.Canceled {
		t.Fatal("control blocked by worker")
	}
	if e.Snapshot().State != BackupExecutionCancelling {
		t.Fatal("worker completed prematurely")
	}
	close(release)
	<-done
	if e.Snapshot().State != BackupExecutionFinished {
		t.Fatal("worker did not complete")
	}
}
