package application

import "context"

// CoverProgressStage describes an entered step, never its success or liveness.
// Keep diagnostics, paths and storage identities out of these fixed codes.
type CoverProgressStage string

const (
	CoverProgressChecking   CoverProgressStage = "checking"
	CoverProgressChoosing   CoverProgressStage = "choosing"
	CoverProgressGenerating CoverProgressStage = "generating"
	CoverProgressPreparing  CoverProgressStage = "preparing"
	CoverProgressSaving     CoverProgressStage = "saving"
	CoverProgressWaiting    CoverProgressStage = "waiting"
	CoverProgressUpdating   CoverProgressStage = "updating"
	CoverProgressCleaning   CoverProgressStage = "cleaning"
	CoverProgressReleasing  CoverProgressStage = "releasing"
)

// CoverProgress is request-local and optional. It runs synchronously, outside
// the admission mutex, but may run while resources/reference exclusion are held.
// Observers must return promptly, not panic, perform I/O or reenter operations.
// They must not change operation state. Results remain the return value's job.
type CoverProgress func(CoverProgressStage)

func (notify CoverProgress) report(stage CoverProgressStage) {
	if notify != nil {
		notify(stage)
	}
}

func (a *CoverImporter) progressStage(ctx context.Context, op *CoverOperation, notify CoverProgress, stage CoverProgressStage, fn func() error) error {
	return a.stage(ctx, op, func() error {
		notify.report(stage)
		return fn()
	})
}
