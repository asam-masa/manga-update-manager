package main

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
)

func TestBackupProgressLifetime(t *testing.T) {
	var s backupRunState
	var ticks atomic.Int64
	base := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return base.Add(time.Duration(ticks.Load())) }
	release, finish := holdBackupRun(t)
	run, err := s.start(context.Background(), "synthetic.zip", func(context.Context, application.BackupExecutionID) (backupSaveRunResult, error) {
		<-release
		return backupSaveRunResult{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	view := s.snapshot()
	if view.execution.ID != run.id || view.execution.State != application.BackupExecutionRunning || !view.execution.CanCancel || view.progress != (backupRunProgress{}) || view.result != nil {
		t.Fatal("missing notification became completion", view)
	}
	ticks.Store(int64(3 * time.Second))
	// Reporting a label must not move the publication/cancellation gate.
	if !s.reportStage(run.id, backupStagePublishing) || s.reportStage(0, backupStageImages) || s.reportStage(run.id, "invalid") {
		t.Fatal("stage admission")
	}
	if got := s.snapshot(); got.progress.Stage != backupStagePublishing || got.progress.Elapsed != 3*time.Second || !got.execution.CanCancel {
		t.Fatal("notification changed control", got)
	}
	if s.cancel(run.id) != application.BackupCancelAccepted {
		t.Fatal("observation committed publication")
	}
	ticks.Store(int64(5 * time.Second))
	if !s.reportStage(run.id, backupStageCleanup) {
		t.Fatal("cleanup observation rejected after cancel")
	}
	if got := s.snapshot(); got.progress.Elapsed != 5*time.Second || got.progress.Stage != backupStageCleanup || got.result != nil || got.execution.CanCancel || got.execution.State != application.BackupExecutionCancelling {
		t.Fatal("cancel cleanup stopped observation", got)
	}
	select {
	case <-run.done:
		t.Fatal("notification completed the run")
	default:
	}
	ticks.Store(int64(9 * time.Second))
	finish()
	waitBackupRun(t, run.done)
	ticks.Store(int64(99 * time.Second))
	if got := s.snapshot(); got.result == nil || got.execution.State != application.BackupExecutionFinished || got.progress.Elapsed != 9*time.Second || got.progress.Stage != backupStageCleanup || s.reportStage(run.id, backupStageZIP) {
		t.Fatal("finished observation changed", got)
	}
	release2, finish2 := holdBackupRun(t)
	next, err := s.start(context.Background(), "next.zip", func(context.Context, application.BackupExecutionID) (backupSaveRunResult, error) {
		<-release2
		return backupSaveRunResult{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.reportStage(run.id, backupStageDatabase) {
		t.Fatal("old notification entered new run")
	}
	if got := s.snapshot(); got.execution.ID != next.id || got.progress != (backupRunProgress{}) || got.result != nil {
		t.Fatal("new run inherited progress", got)
	}
	s.close()
	if s.reportStage(next.id, backupStageCleanup) || s.snapshot().progress != (backupRunProgress{}) {
		t.Fatal("closed owner retained progress")
	}
	finish2()
	waitBackupRun(t, next.done)
	if s.snapshot().execution.ID != 0 || s.snapshot().progress != (backupRunProgress{}) {
		t.Fatal("late finish revived progress")
	}
}

func TestBackupProgressClockOutsideLock(t *testing.T) {
	var s backupRunState
	// Calls are serialized by the held worker/done channel in this test. A clock
	// trying to reenter the owner must not find its own caller holding the mutex.
	s.now = func() time.Time {
		if !s.mu.TryLock() {
			t.Error("clock invoked under owner lock")
		} else {
			s.mu.Unlock()
		}
		return time.Time{}
	}
	release, finish := holdBackupRun(t)
	run, err := s.start(context.Background(), "synthetic.zip", func(context.Context, application.BackupExecutionID) (backupSaveRunResult, error) {
		<-release
		return backupSaveRunResult{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	s.snapshot()
	finish()
	waitBackupRun(t, run.done)
	s.snapshot()
}

func TestBackupProgressConcurrentObservation(t *testing.T) {
	var s backupRunState
	var ticks atomic.Int64
	s.now = func() time.Time { return time.Time{}.Add(time.Duration(ticks.Load())) }
	release, finish := holdBackupRun(t)
	run, err := s.start(context.Background(), "synthetic.zip", func(context.Context, application.BackupExecutionID) (backupSaveRunResult, error) {
		<-release
		return backupSaveRunResult{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 20 {
				ticks.Add(1)
				s.reportStage(run.id, backupStageCleanup)
				s.cancel(run.id)
				got := s.snapshot()
				if got.execution.ID != run.id || got.result != nil || got.execution.State == application.BackupExecutionFinished || got.progress.Elapsed < 0 {
					t.Error("incoherent observation", got)
				}
			}
		})
	}
	wg.Wait()
	// A backwards synthetic clock never exposes a negative duration.
	ticks.Store(-1)
	if got := s.snapshot(); got.progress.Elapsed != 0 {
		t.Fatal(got)
	}
	finish()
	waitBackupRun(t, run.done)
}
