package main

import (
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
)

type backupStage = application.BackupStage

const (
	backupStageWaiting       = application.BackupStageWaiting
	backupStagePreparing     = application.BackupStagePreparing
	backupStageDatabase      = application.BackupStageDatabase
	backupStageDatabaseCheck = application.BackupStageDatabaseCheck
	backupStageImages        = application.BackupStageImages
	backupStageMetadata      = application.BackupStageMetadata
	backupStageZIP           = application.BackupStageZIP
	backupStagePublishing    = application.BackupStagePublishing
	backupStageCleanup       = application.BackupStageCleanup
)

// Empty Stage means no stage was observed, never completion or a frozen worker.
// Elapsed is since start acceptance, including cancellation cleanup/result copy.
// This is observation only: cancellation permission comes from execution state.
type backupRunProgress struct {
	Stage   backupStage
	Elapsed time.Duration
}

// now is fixed before first use; injected clocks must be concurrency-safe.
// Clock callbacks run outside mu, including during start/finish/snapshot.
func (s *backupRunState) clockNow() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func backupElapsed(start, now time.Time) time.Duration {
	return max(0, now.Sub(start))
}

// reportStage records only an actual entered stage of this live run. Stages may
// repeat: DB cleanup can precede publication, followed by output cleanup. Never
// reorder work or advance the execution gate to make the display look linear.
func (s *backupRunState) reportStage(id application.BackupExecutionID, stage backupStage) bool {
	switch stage {
	case backupStageWaiting, backupStagePreparing, backupStageDatabase, backupStageDatabaseCheck, backupStageImages, backupStageMetadata, backupStageZIP, backupStagePublishing, backupStageCleanup:
	default:
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.executions.Snapshot()
	if s.closed || id == 0 || state.ID != id || state.State == application.BackupExecutionFinished {
		return false
	}
	s.stage = stage
	return true
}
