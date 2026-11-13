package main

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/asam-masa/manga-update-manager/internal/application"
)

var errBackupWorkerRequired = errors.New("backup worker required")

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
}

// backupRunState retains one run in memory. Do not copy after use. The worker
// owns its I/O and must return only after all cleanup/release attempts. Errors
// and their diagnostic chains are read-only; mutable result metadata is copied.
// This is not wired into App, OperationGate, native publication or Wails yet.
type backupRunState struct {
	mu          sync.Mutex
	executions  application.BackupExecutions
	destination string
	result      *backupRunResult // Immutable after assignment; cloned outside mu.
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
	s.mu.Lock()
	id, ctx, err := s.executions.Begin(appContext)
	if err != nil {
		s.mu.Unlock()
		return backupRun{}, err
	}
	s.destination, s.result = destination, nil
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.executions.Finish(id) {
		// Snapshot cannot observe Finished without its matching result.
		s.result = owned
	}
}

func (s *backupRunState) snapshot() backupRunSnapshot {
	s.mu.Lock()
	view := backupRunSnapshot{execution: s.executions.Snapshot(), destination: s.destination}
	stored := s.result
	s.mu.Unlock()
	if stored != nil {
		view.result = &backupRunResult{save: cloneBackupSaveRunResult(stored.save), err: stored.err}
	}
	return view
}

func (s *backupRunState) cancel(id application.BackupExecutionID) application.BackupCancelDecision {
	return s.executions.Cancel(id)
}

// The eventual save worker must call this immediately before native publication.
// This component alone does not prevent an ungated native call.
func (s *backupRunState) beginPublication(id application.BackupExecutionID) bool {
	return s.executions.BeginPublication(id)
}

func cloneBackupSaveRunResult(result backupSaveRunResult) backupSaveRunResult {
	result.Warnings = slices.Clone(result.Warnings)
	result.Manifest = bytes.Clone(result.Manifest)
	result.Covers.Images = slices.Clone(result.Covers.Images)
	result.Covers.OmittedImages = slices.Clone(result.Covers.OmittedImages)
	if result.Save.Cleanup != nil {
		cleanup := *result.Save.Cleanup
		result.Save.Cleanup = &cleanup
	}
	return result
}
