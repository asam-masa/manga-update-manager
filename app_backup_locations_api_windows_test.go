package main

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/platform"
)

func TestBackupLocationsObservations(t *testing.T) {
	for _, mode := range []string{"confirmed", "unknown", "future", "collision", "absent", "residue", "creation"} {
		t.Run(mode, func(t *testing.T) {
			var a App
			r := backupSaveRunResult{}
			r.Save.Publication = platform.BackupPublication{TemporaryPath: "zip.partial", FinalPath: "target.zip"}
			r.Database.Cleanup.File.Path = "copy.sqlite"
			r.Database.Cleanup.Directory.Path = "work"
			want := []BackupLocationDTO{{"temporary_zip", "zip.partial"}, {"database_copy", "copy.sqlite"}, {"work_directory", "work"}}
			publication, confirmed := "not_attempted", ""
			switch mode {
			case "confirmed":
				r.Save.Publication.State = platform.BackupPublishConfirmed
				publication, confirmed = "confirmed", "target.zip"
				want = want[1:]
			case "unknown", "future":
				r.Save.Publication.State = platform.BackupPublishUnknown
				if mode == "future" {
					r.Save.Publication.State = 255
				}
				publication = "unknown"
				want = append(want[:1:1], append([]BackupLocationDTO{{"publication_target", "private-destination"}, {"publication_target", "target.zip"}}, want[1:]...)...)
			case "collision":
				r.Save.Publication.State = platform.BackupPublishCollision
				publication = "collision"
			case "absent":
				r.Save.Cleanup = &platform.BackupDiscardResult{TemporaryPath: "zip.partial", Absent: true}
				r.Database.Cleanup.File.Absent = true
				r.Database.Cleanup.Directory.Absent = true
				want = []BackupLocationDTO{}
			case "residue":
				r.Save.Cleanup = &platform.BackupDiscardResult{TemporaryPath: "zip.partial", MarkedForDeletion: true}
			case "creation":
				r = backupSaveRunResult{}
				r.OutputCreationError = &platform.BackupTempError{Path: "zip.partial", Err: errors.New("private-diagnostic")}
				r.CreationError = &platform.BackupDatabaseWorkError{OutputPath: "copy.sqlite", WorkPath: "work", Err: &platform.BackupWorkDirectoryError{Path: "work", Err: errors.New("private-diagnostic")}}
			}
			r.FinishError = errors.New("private-diagnostic")
			id := retainBackupAPIResult(t, &a, r)
			before := a.backupRuns.snapshot()
			got, err := a.GetBackupLocations(id)
			if err != nil || got.ExecutionID != id || got.Destination != "private-destination" || got.Publication != publication || got.ConfirmedPath != confirmed || !reflect.DeepEqual(got.Candidates, want) {
				t.Fatal(got, want, err)
			}
			data, _ := json.Marshal(got)
			if strings.Contains(string(data), "private-diagnostic") {
				t.Fatal("diagnostic leaked")
			}
			if len(got.Candidates) > 0 {
				got.Candidates[0].Path = "changed"
			}
			if !reflect.DeepEqual(before, a.backupRuns.snapshot()) {
				t.Fatal("read mutated result")
			}
		})
	}
}

func TestBackupLocationsErrors(t *testing.T) {
	var a App
	if _, err := a.GetBackupLocations("01"); errorCode(t, err) != "invalid_input" {
		t.Fatal(err)
	}
	if _, err := a.GetBackupLocations("1"); errorCode(t, err) != "backup_result_expired" {
		t.Fatal(err)
	}
	release, finish := holdBackupRun(t)
	run, err := a.backupRuns.start(context.Background(), "destination", func(context.Context, application.BackupExecutionID) (backupSaveRunResult, error) {
		<-release
		return backupSaveRunResult{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.GetBackupLocations("1"); errorCode(t, err) != "backup_result_pending" {
		t.Fatal(err)
	}
	finish()
	waitBackupRun(t, run.done)
	retainBackupAPIResult(t, &a, backupSaveRunResult{})
	if _, err := a.GetBackupLocations("1"); errorCode(t, err) != "backup_result_expired" {
		t.Fatal(err)
	}
}
