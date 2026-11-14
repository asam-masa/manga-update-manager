package main

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/platform"
)

var errBackupWorkerRequired = errors.New("backup worker required")
var errBackupRunsClosed = errors.New("backup execution owner closed")
var errBackupRetryUnsafe = errors.New("backup publication must be checked before retry")

type backupRun struct {
	id   application.BackupExecutionID
	done <-chan struct{} // Closes only after the outcome is stored.
}

type backupRunResult struct {
	save backupSaveRunResult
	err  error // Internal immutable diagnostic; not a public DTO.
}

type backupRunSnapshot struct {
	execution   application.BackupExecutionSnapshot
	destination string
	result      *backupRunResult // Nil while running, including cancellation cleanup.
	progress    backupRunProgress
}

// backupRunState retains one run in memory. Do not copy after use. The worker
// owns its I/O and must return only after all cleanup/release attempts. Errors
// and their diagnostic chains are read-only; mutable result metadata is copied.
// App owns this state; native publication and Wails are not connected yet.
type backupRunState struct {
	mu          sync.Mutex
	executions  application.BackupExecutions
	destination string
	result      *backupRunResult // Immutable after assignment; cloned outside mu.
	closed      bool
	now         func() time.Time // Fixed before use; never invoked under mu.
	startedAt   time.Time
	elapsed     time.Duration // Frozen only when result storage finishes.
	stage       backupStage
}

// start is called only after destination selection and start admission. A caller
// must supply the application's lifetime context, never a dialog/request context.
// No worker/IO or result cloning runs under mu. A rejected start preserves the
// previous snapshot; merely selecting/cancelling a destination never calls this.
func (s *backupRunState) start(appContext context.Context, destination string,
	work func(context.Context, application.BackupExecutionID) (backupSaveRunResult, error),
) (backupRun, error) {
	if work == nil {
		return backupRun{}, errBackupWorkerRequired
	}
	now := s.clockNow()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return backupRun{}, errBackupRunsClosed
	}
	if s.result != nil {
		switch s.result.save.Save.Publication.State {
		case platform.BackupPublishNotAttempted, platform.BackupPublishCollision, platform.BackupPublishConfirmed:
		default:
			s.mu.Unlock()
			return backupRun{}, errBackupRetryUnsafe
		}
	}
	id, ctx, err := s.executions.Begin(appContext)
	if err != nil {
		s.mu.Unlock()
		return backupRun{}, err
	}
	s.destination, s.result = destination, nil
	s.startedAt, s.elapsed, s.stage = now, 0, ""
	s.mu.Unlock()
	done := make(chan struct{})
	go func() {
		result, err := work(ctx, id)
		s.finish(id, result, err)
		close(done)
	}()
	return backupRun{id: id, done: done}, nil
}

func (s *backupRunState) finish(id application.BackupExecutionID, result backupSaveRunResult, err error) {
	owned := &backupRunResult{save: cloneBackupSaveRunResult(result), err: err}
	now := s.clockNow()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.executions.Finish(id) && !s.closed {
		// Snapshot cannot observe Finished without its matching result.
		s.result = owned
		s.elapsed = backupElapsed(s.startedAt, now)
	}
}

func (s *backupRunState) snapshot() backupRunSnapshot {
	view, stored := s.inspect()
	if stored != nil {
		view.result = &backupRunResult{save: cloneBackupSaveRunResult(stored.save), err: stored.err}
	}
	return view
}

// inspect captures one generation without cloning its potentially large result.
// stored is a private immutable borrow: never expose it in an observable view.
func (s *backupRunState) inspect() (backupRunSnapshot, *backupRunResult) {
	now := s.clockNow()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return backupRunSnapshot{}, nil
	}
	view := backupRunSnapshot{execution: s.executions.Snapshot(), destination: s.destination}
	if view.execution.ID != 0 {
		view.progress = backupRunProgress{Stage: s.stage, Elapsed: s.elapsed}
		if view.execution.State != application.BackupExecutionFinished {
			view.progress.Elapsed = backupElapsed(s.startedAt, now)
		}
	}
	stored := s.result
	s.mu.Unlock()
	return view, stored
}

func (s *backupRunState) cancel(id application.BackupExecutionID) application.BackupCancelDecision {
	return s.executions.Cancel(id)
}

// The eventual save worker must call this immediately before native publication.
// This component alone does not prevent an ungated native call.
func (s *backupRunState) beginPublication(id application.BackupExecutionID) bool {
	return s.executions.BeginPublication(id)
}

// busy is a short control check; do not clone outcome data in RequestClose.
// It never acquires App or OperationGate locks.
func (s *backupRunState) busy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.executions.Snapshot().State
	return state != application.BackupExecutionIdle && state != application.BackupExecutionFinished
}

// Normal shutdown is admitted only after completion. If shutdown bypasses that
// protocol, request cooperative cancellation but never revive a late result.
func (s *backupRunState) close() {
	s.mu.Lock()
	s.closed = true
	s.result, s.destination = nil, ""
	s.startedAt, s.elapsed, s.stage = time.Time{}, 0, ""
	id := s.executions.Snapshot().ID
	s.mu.Unlock()
	s.executions.Cancel(id)
}

func cloneBackupSaveRunResult(result backupSaveRunResult) backupSaveRunResult {
	result.Warnings = slices.Clone(result.Warnings)
	result.Manifest = bytes.Clone(result.Manifest)
	result.Covers.Images = slices.Clone(result.Covers.Images)
	result.Covers.OmittedImages = slices.Clone(result.Covers.OmittedImages)
	result.Omissions.Images = slices.Clone(result.Omissions.Images)
	result.Omissions.Works = slices.Clone(result.Omissions.Works)
	if result.Save.Cleanup != nil {
		cleanup := *result.Save.Cleanup
		result.Save.Cleanup = &cleanup
	}
	return result
}
