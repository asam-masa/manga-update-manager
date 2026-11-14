package main

import (
	"context"
	"errors"
	"slices"

	"github.com/asam-masa/manga-update-manager/internal/application"
)

// startBackupRun is internal and accepts only an approved destination. The
// worker borrows admission, owns its resources, and must return after cleanup.
// released reports all worker-owned releases, never merely err == nil. App
// combines it with existing DB/image stop checks before resuming normal work.
// Real saving/publication, destination selection and Wails remain unconnected.
func (a *App) startBackupRun(destination string,
	work func(context.Context, application.BackupExecutionID) (result backupSaveRunResult, released bool, err error),
) (backupRun, error) {
	if work == nil {
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
	if err := a.checkBackupStart(ctx); err != nil {
		op.End()
		return backupRun{}, err // Rejection must not erase the previous result.
	}
	run, err := a.backupRuns.start(ctx, destination, func(ctx context.Context, id application.BackupExecutionID) (backupSaveRunResult, error) {
		defer op.End() // Preflight failure; finished worker tickets are idempotent.
		a.backupRuns.reportStage(id, backupStageWaiting)
		warnings, err := a.prepareReservedBackup(ctx, op)
		if err != nil {
			var result backupSaveRunResult
			result.Warnings = warnings
			return result, err
		}
		result, released, err := work(ctx, id)
		result.Warnings = append(slices.Clone(warnings), result.Warnings...)
		result.FinishError = errors.Join(result.FinishError, a.finishBackup(op, released))
		return result, errors.Join(err, result.FinishError)
	})
	if err != nil {
		op.End() // No worker or I/O was started; preserve the previous result.
	}
	return run, err
}

// Observe existing stop latches before acceptance, without requiring cleanup to
// have run or all earlier operations to be idle. A later stop is checked again
// after cleanup/drain and becomes the accepted run's outcome.
func (a *App) checkBackupStart(ctx context.Context) error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.stopped || a.store == nil || a.covers == nil {
		return unavailableError()
	}
	if err := a.store.CheckAdmission(); err != nil {
		return err
	}
	if err := a.coverOperations.CheckIdle(); err != nil && !errors.Is(err, application.ErrCoverBusy) {
		return err
	}
	return ctx.Err()
}
