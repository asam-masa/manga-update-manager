package application

import (
	"context"
	"errors"
	"sync"
)

var ErrBackupExecutionIDExhausted = errors.New("backup execution identifiers exhausted")

type BackupExecutionID uint64
type BackupExecutionState string

const (
	BackupExecutionIdle       BackupExecutionState = ""
	BackupExecutionRunning    BackupExecutionState = "running"
	BackupExecutionCancelling BackupExecutionState = "cancelling"
	BackupExecutionPublishing BackupExecutionState = "publishing"
	BackupExecutionFinished   BackupExecutionState = "finished"
)

type BackupCancelDecision string

const (
	BackupCancelAccepted BackupCancelDecision = "accepted"
	BackupCancelRepeated BackupCancelDecision = "already_accepted"
	BackupCancelTooLate  BackupCancelDecision = "publication_started"
	BackupCancelFinished BackupCancelDecision = "finished"
	BackupCancelStale    BackupCancelDecision = "stale"
)

// BackupExecutionSnapshot is a value copy. Finished means the owner has returned
// from cleanup/release, not that saving succeeded or every release succeeded.
type BackupExecutionSnapshot struct {
	ID        BackupExecutionID
	State     BackupExecutionState
	CanCancel bool
}

// BackupExecutions orders control decisions only. Its zero value is ready; do
// not copy after use. It does not run work, own files, reserve OperationGate, or
// retain save results. The caller must connect the publication gate immediately
// before native publication and finish only after all cleanup/release attempts.
// No I/O, callbacks, or waits run under mu.
type BackupExecutions struct {
	mu     sync.Mutex
	id     BackupExecutionID
	state  BackupExecutionState
	ctx    context.Context
	cancel context.CancelFunc
}

// Begin accepts a new execution and replaces the previous finished snapshot only
// on success. appContext must follow the application's lifetime, not a request,
// dialog, or WebView reload. Cancellation never finishes an execution itself.
func (e *BackupExecutions) Begin(appContext context.Context) (BackupExecutionID, context.Context, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := appContext.Err(); err != nil {
		return 0, nil, err
	}
	if e.state != BackupExecutionIdle && e.state != BackupExecutionFinished {
		return 0, nil, ErrBackupBusy
	}
	if e.id == ^BackupExecutionID(0) {
		return 0, nil, ErrBackupExecutionIDExhausted
	}
	e.id++
	e.ctx, e.cancel = context.WithCancel(appContext)
	e.state = BackupExecutionRunning
	return e.id, e.ctx, nil
}

func (e *BackupExecutions) Snapshot() BackupExecutionSnapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	return BackupExecutionSnapshot{ID: e.id, State: e.state,
		CanCancel: e.state == BackupExecutionRunning && e.ctx.Err() == nil}
}

func (e *BackupExecutions) Cancel(id BackupExecutionID) BackupCancelDecision {
	e.mu.Lock()
	if id == 0 || id != e.id {
		e.mu.Unlock()
		return BackupCancelStale
	}
	var decision BackupCancelDecision
	switch e.state {
	case BackupExecutionRunning:
		e.state = BackupExecutionCancelling
		decision = BackupCancelAccepted
	case BackupExecutionCancelling:
		decision = BackupCancelRepeated
	case BackupExecutionPublishing:
		decision = BackupCancelTooLate
	default:
		decision = BackupCancelFinished
	}
	cancel := e.cancel
	e.mu.Unlock()
	if decision == BackupCancelAccepted || decision == BackupCancelRepeated {
		cancel() // Only this run's context; never under the state lock.
	}
	return decision
}

// BeginPublication and Cancel share one lock: an accepted cancel always denies
// publication. A true result is consumed once; no caller may retry publication.
// Parent cancellation is cooperative and may still arrive after this decision.
func (e *BackupExecutions) BeginPublication(id BackupExecutionID) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if id == 0 || id != e.id || e.state != BackupExecutionRunning || e.ctx.Err() != nil {
		return false
	}
	e.state = BackupExecutionPublishing
	return true
}

// Finish is called by the worker after all cleanup/release attempts, including
// failures. Its boolean is identity acceptance, never permission to resume normal
// operations. Keep release/stop checks and outcome storage in the owning layer.
func (e *BackupExecutions) Finish(id BackupExecutionID) bool {
	e.mu.Lock()
	if id == 0 || id != e.id || e.state == BackupExecutionFinished {
		e.mu.Unlock()
		return false
	}
	e.state = BackupExecutionFinished
	cancel := e.cancel
	e.mu.Unlock()
	cancel()
	return true
}
