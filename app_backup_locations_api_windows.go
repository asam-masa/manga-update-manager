package main

import (
	"errors"
	"strconv"

	"github.com/asam-masa/manga-update-manager/internal/platform"
)

type BackupLocationDTO struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
}

type BackupLocationsDTO struct {
	ExecutionID   string              `json:"executionId"`
	Destination   string              `json:"destination"`
	Publication   string              `json:"publication"`
	ConfirmedPath string              `json:"confirmedPath"`
	Candidates    []BackupLocationDTO `json:"candidates"`
}

// Locations are retained observations, never authority to reopen or delete.
func (a *App) GetBackupLocations(executionID string) (BackupLocationsDTO, error) {
	id, err := parseBackupExecutionID(executionID)
	if err != nil {
		return BackupLocationsDTO{}, backupDetailAPIError(err)
	}
	v := a.backupRuns.overview()
	if v.execution.ID != id {
		return BackupLocationsDTO{}, backupDetailAPIError(errBackupDetailStale)
	}
	if v.result == nil {
		return BackupLocationsDTO{}, backupDetailAPIError(errBackupDetailPending)
	}
	return backupLocationsDTO(strconv.FormatUint(uint64(id), 10), v.destination, v.result), nil
}

func backupLocationsDTO(id, destination string, r *backupResultOverview) BackupLocationsDTO {
	out := BackupLocationsDTO{ExecutionID: id, Destination: destination, Publication: "unknown", Candidates: []BackupLocationDTO{}}
	add := func(kind, path string) {
		if path == "" {
			return
		}
		item := BackupLocationDTO{Kind: kind, Path: path}
		for _, existing := range out.Candidates {
			if existing == item {
				return
			}
		}
		out.Candidates = append(out.Candidates, item)
	}
	p := r.Save.Publication
	switch p.State {
	case platform.BackupPublishConfirmed:
		out.Publication = "confirmed"
	case platform.BackupPublishCollision:
		out.Publication = "collision"
	case platform.BackupPublishNotAttempted:
		out.Publication = "not_attempted"
	}
	if p.State == platform.BackupPublishConfirmed {
		out.ConfirmedPath = p.FinalPath
	} else {
		if r.Save.Cleanup == nil || !r.Save.Cleanup.Absent {
			add("temporary_zip", p.TemporaryPath)
			if r.Save.Cleanup != nil {
				add("temporary_zip", r.Save.Cleanup.TemporaryPath)
			}
		}
		if p.State != platform.BackupPublishNotAttempted && p.State != platform.BackupPublishCollision {
			add("publication_target", destination)
			add("publication_target", p.FinalPath)
		}
	}
	var output *platform.BackupTempError
	if errors.As(r.OutputCreationError, &output) {
		add("temporary_zip", output.Path)
	}
	c := r.Database.Cleanup
	if !c.File.Absent {
		add("database_copy", c.File.Path)
	}
	if !c.Directory.Absent {
		add("work_directory", c.Directory.Path)
	}
	var work *platform.BackupDatabaseWorkError
	if errors.As(r.CreationError, &work) {
		add("database_copy", work.OutputPath)
		add("work_directory", work.WorkPath)
	}
	var directory *platform.BackupWorkDirectoryError
	if errors.As(r.CreationError, &directory) {
		add("work_directory", directory.Path)
	}
	return out
}
