package main

import (
	"context"
	"errors"

	"github.com/asam-masa/manga-update-manager/internal/application"
)

var errBackupCleanupUnverified = errors.New("backup startup cleanup unverified")
var errBackupReleaseUnverified = errors.New("backup resource release unverified")

// prepareBackupCleanup does not reserve backup admission or permit acquisition.
// The caller separately owns close exclusion, then drains normal operations and
// checks DB/image stop state after cleanup returns.
// Once cleanup starts, cancellation is observed after joining its resource release;
// it never rolls back or abandons cleanup in a detached goroutine.
func (a *App) prepareBackupCleanup(ctx context.Context) ([]CoverWarningDTO, error) {
	select {
	case <-a.startupFinished:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	warnings, err := a.LoadCoverWarnings()
	if err != nil {
		return warnings, errors.Join(err, ctx.Err())
	}
	if len(warnings) != 0 || a.coverCleanupPhase.Load() != 1 {
		return warnings, errors.Join(errBackupCleanupUnverified, ctx.Err())
	}
	return warnings, ctx.Err()
}

// beginBackup holds the reservation through acquisition, saving and all resource
// release by its caller. No public Wails API exposes this internal entry yet.
func (a *App) beginBackup(ctx context.Context) (*application.BackupOperation, []CoverWarningDTO, error) {
	// Before startup completes there is no accepted backup reservation. Preserve
	// normal close as a possible release of the startup wait.
	select {
	case <-a.startupFinished:
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
	op, err := a.operationGate.ReserveBackup(ctx)
	if err != nil {
		return nil, nil, err
	}
	warnings, err := a.prepareReservedBackup(ctx, op)
	if err != nil {
		op.End()
		return nil, warnings, err
	}
	return op, warnings, nil
}

// The caller owns op through this call and ends it on failure only after cleanup
// has returned. This shared path does not start a second reservation.
func (a *App) prepareReservedBackup(ctx context.Context, op *application.BackupOperation) ([]CoverWarningDTO, error) {
	// Cleanup enters as a normal operation. Reserve close/duplicates first, but
	// do not pause normal admission until cleanup and all of its releases return.
	warnings, err := a.prepareBackupCleanup(ctx)
	if err != nil {
		return warnings, err
	}
	if err := op.PauseAndWait(ctx); err != nil {
		return warnings, err
	}
	if err := a.checkBackupState(ctx); err != nil {
		return warnings, err
	}
	return warnings, nil
}

func (a *App) checkBackupState(ctx context.Context) error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.stopped || a.store == nil || a.covers == nil {
		return unavailableError()
	}
	if a.coverCleanupPhase.Load() != 1 {
		return errBackupCleanupUnverified
	}
	if err := a.store.CheckAdmission(); err != nil {
		return err
	}
	if err := a.coverOperations.CheckIdle(); err != nil {
		return err
	}
	return ctx.Err()
}

// finishBackup owns a live ticket returned by this App's beginBackup. Call only
// after all cleanup/release attempts finish, using ReleaseConfirmed from the
// owned result/error. Keep original operation errors and warnings separately.
// Operation cancellation is not evidence about release or restart permission.
func (a *App) finishBackup(op *application.BackupOperation, releaseConfirmed bool) error {
	err := a.checkBackupState(context.Background())
	if !releaseConfirmed {
		err = errors.Join(errBackupReleaseUnverified, err)
	}
	op.EndWithResume(err == nil)
	return err
}
