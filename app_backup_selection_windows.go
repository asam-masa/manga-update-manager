package main

import (
	"context"

	"github.com/asam-masa/manga-update-manager/internal/application"
)

// selectBackupRun is an internal seam for the later native save picker/API.
// It never accepts a path from the frontend. Empty selection means no start.
// Picker callbacks must return only after their native resources are released.
func (a *App) selectBackupRun(choose func(context.Context) (string, error),
	work func(context.Context, application.BackupExecutionID) (backupSaveRunResult, bool, error),
) (backupRun, error) {
	if choose == nil || work == nil {
		return backupRun{}, errBackupWorkerRequired
	}
	a.mu.RLock()
	ctx := a.ctx
	ready := !a.stopped && ctx != nil && a.store != nil && a.covers != nil
	a.mu.RUnlock()
	if !ready {
		return backupRun{}, unavailableError()
	}
	op, err := a.operationGate.ReserveBackup(ctx)
	if err != nil {
		return backupRun{}, err
	}
	defer func() {
		if op != nil {
			op.End()
		}
	}()
	if err := a.checkBackupStart(ctx); err != nil {
		return backupRun{}, err
	}
	path, err := a.fileSelections.choose(ctx, choose)
	if err != nil || path == "" {
		return backupRun{}, err
	}
	// The callee consumes ownership even if acceptance is rejected.
	reserved := op
	op = nil
	return a.startReservedBackupRun(ctx, reserved, path, work)
}
