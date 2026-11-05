package main

import (
	"context"
	"errors"
)

var errBackupCleanupUnverified = errors.New("backup startup cleanup unverified")

// prepareBackupCleanup does not reserve backup admission or permit acquisition.
// The caller must subsequently reserve, drain, and check DB/image stop state.
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
