package main

import (
	"strconv"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/platform"
)

type BackupStatusDTO struct {
	ExecutionID    string                   `json:"executionId"`
	State          string                   `json:"state"`
	Stage          string                   `json:"stage"`
	ElapsedSeconds int64                    `json:"elapsedSeconds"`
	CanCancel      bool                     `json:"canCancel"`
	Result         *BackupObservedResultDTO `json:"result"`
}

// These are observations, not permission to retry/resume or a success verdict.
// Unknown counts are null. No raw diagnostics, artifact bytes or paths escape.
type BackupObservedResultDTO struct {
	Publication       string `json:"publication"`
	HasWarnings       bool   `json:"hasWarnings"`
	OmittedImageCount *int   `json:"omittedImageCount"`
	WorkNamesState    string `json:"workNamesState"`
	WorkCount         *int   `json:"workCount"`
}

// GetBackupStatus never takes normal admission or performs DB/file I/O. The
// owner captures one immutable generation; a caller must compare response IDs.
func (a *App) GetBackupStatus() BackupStatusDTO {
	v := a.backupRuns.overview()
	out := BackupStatusDTO{State: "unknown", Stage: "unobserved", ElapsedSeconds: max(0, int64(v.progress.Elapsed/time.Second))}
	if v.execution.ID != 0 {
		out.ExecutionID = strconv.FormatUint(uint64(v.execution.ID), 10)
	}
	switch v.execution.State {
	case application.BackupExecutionIdle:
		out.State = "idle"
	case application.BackupExecutionRunning, application.BackupExecutionCancelling, application.BackupExecutionPublishing, application.BackupExecutionFinished:
		out.State = string(v.execution.State)
	}
	switch v.progress.Stage {
	case backupStageWaiting, backupStagePreparing, backupStageDatabase, backupStageDatabaseCheck, backupStageImages, backupStageMetadata, backupStageZIP, backupStagePublishing, backupStageCleanup:
		out.Stage = string(v.progress.Stage)
	}
	out.CanCancel = v.execution.State == application.BackupExecutionRunning && v.execution.CanCancel
	if v.result == nil {
		return out
	}
	r := v.result
	result := &BackupObservedResultDTO{Publication: "unknown", WorkNamesState: "unavailable",
		OmittedImageCount: r.OmittedImageCount, WorkCount: r.WorkCount}
	switch r.Save.Publication.State {
	case platform.BackupPublishNotAttempted:
		result.Publication = "not_attempted"
	case platform.BackupPublishCollision:
		result.Publication = "collision"
	case platform.BackupPublishConfirmed:
		result.Publication = "confirmed"
	}
	switch r.WorkNamesState {
	case backupWorkNamesUnobserved:
		result.WorkNamesState = "unobserved"
	case backupWorkNamesAvailable:
		result.WorkNamesState = "available"
	}
	result.HasWarnings = hasBackupResultWarnings(r)
	out.Result = result
	return out
}

func hasBackupResultWarnings(r *backupResultOverview) bool {
	return r.Err != nil || r.DetailError != nil || r.CreationError != nil ||
		r.FinishError != nil || r.OutputCreationError != nil || len(r.Warnings) != 0 ||
		(r.Save.Publication.State != platform.BackupPublishNotAttempted && r.Save.Publication.State != platform.BackupPublishCollision && r.Save.Publication.State != platform.BackupPublishConfirmed) ||
		(r.WorkNamesState != backupWorkNamesUnobserved && r.WorkNamesState != backupWorkNamesAvailable) ||
		(r.OmittedImageCount != nil && *r.OmittedImageCount > 0)
}
