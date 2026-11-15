package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/application"
)

func retainBackupAPIResult(t *testing.T, a *App, result backupSaveRunResult) string {
	t.Helper()
	run, err := a.backupRuns.start(context.Background(), "private-destination", func(context.Context, application.BackupExecutionID) (backupSaveRunResult, error) { return result, nil })
	if err != nil {
		t.Fatal(err)
	}
	waitBackupRun(t, run.done)
	return strconv.FormatUint(uint64(run.id), 10)
}

func TestBackupAPIExecutionID(t *testing.T) {
	for _, value := range []string{"1", "9007199254740993", "18446744073709551615"} {
		id, err := parseBackupExecutionID(value)
		if err != nil || strconv.FormatUint(uint64(id), 10) != value {
			t.Fatal(value, id, err)
		}
		data, err := json.Marshal(BackupCancelDTO{ExecutionID: value})
		if err != nil || !strings.Contains(string(data), `"executionId":"`+value+`"`) {
			t.Fatal(string(data), err)
		}
	}
	var a App
	before := a.backupRuns.snapshot()
	for _, value := range []string{"", "0", "-1", "+1", "01", " 1", "1 ", "１", "1e3", "1.0", "18446744073709551616", strings.Repeat("9", 100)} {
		if _, err := a.CancelBackup(value); errorCode(t, err) != "invalid_input" {
			t.Fatal(value, err)
		}
		if _, err := a.GetBackupOmittedImages(value, 1); errorCode(t, err) != "invalid_input" {
			t.Fatal(value, err)
		}
		if _, err := a.GetBackupOmittedImageWorks(value, strings.Repeat("a", 32), 1); errorCode(t, err) != "invalid_input" {
			t.Fatal(value, err)
		}
	}
	if !reflect.DeepEqual(before, a.backupRuns.snapshot()) {
		t.Fatal("invalid input mutated execution")
	}
}

func TestBackupAPICancelWithoutNormalAdmission(t *testing.T) {
	for _, publishing := range []bool{false, true} {
		t.Run(fmt.Sprint(publishing), func(t *testing.T) {
			var a App
			op, err := a.operationGate.BeginBackup(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer op.End()
			release, finish := holdBackupRun(t)
			run, err := a.backupRuns.start(context.Background(), "private", func(context.Context, application.BackupExecutionID) (backupSaveRunResult, error) {
				<-release
				return backupDetailsFixture(1, 1), nil
			})
			if err != nil {
				t.Fatal(err)
			}
			id := strconv.FormatUint(uint64(run.id), 10)
			if publishing && !a.backupRuns.beginPublication(run.id) {
				t.Fatal("publication refused")
			}
			want := "accepted"
			if publishing {
				want = "publication_started"
			}
			result, err := a.CancelBackup(id)
			if err != nil || result.ExecutionID != id || result.Decision != want {
				t.Fatal(result, err)
			}
			if !publishing {
				if repeated, err := a.CancelBackup(id); err != nil || repeated.Decision != "already_accepted" {
					t.Fatal(repeated, err)
				}
			}
			if _, err := a.GetBackupOmittedImages(id, 1); errorCode(t, err) != "backup_result_pending" {
				t.Fatal(err)
			}
			finish()
			waitBackupRun(t, run.done)
			if result, err := a.CancelBackup(id); err != nil || result.Decision != "finished" {
				t.Fatal(result, err)
			}
			op.EndWithResume(false) // Observation/control must also survive a safety stop.
			if _, err := a.GetBackupOmittedImages(id, 1); err != nil {
				t.Fatal(err)
			}
			_ = retainBackupAPIResult(t, &a, backupDetailsFixture(1, 1))
			before := a.backupRuns.snapshot()
			if _, err := a.CancelBackup(id); errorCode(t, err) != "backup_result_expired" {
				t.Fatal(err)
			}
			if _, err := a.GetBackupOmittedImages(id, 1); errorCode(t, err) != "backup_result_expired" {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, a.backupRuns.snapshot()) {
				t.Fatal("old request affected next run")
			}
		})
	}
}

func TestBackupAPIDetailDTOs(t *testing.T) {
	var a App
	r := backupDetailsFixture(51, 51)
	const private = "private-diagnostic-or-path"
	r.Omissions.Images[0].Path = private
	r.Omissions.Images[0].Reason = private
	r.Omissions.DetailError = errors.New(private)
	r.CreationError = errors.New(private)
	r.Omissions.Works[0].WorkID = 9223372036854775807
	id := retainBackupAPIResult(t, &a, r)
	before := a.backupRuns.snapshot()
	first, err := a.GetBackupOmittedImages(id, 1)
	if err != nil || first.ExecutionID != id || first.PageSize != 50 || first.Pages != 2 || first.Total != 51 || len(first.Items) != 50 || first.Items[0].Reason != "unavailable" {
		t.Fatal(first, err)
	}
	last, err := a.GetBackupOmittedImages(id, 2)
	if err != nil || len(last.Items) != 1 {
		t.Fatal(last, err)
	}
	works, err := a.GetBackupOmittedImageWorks(id, first.Items[0].ImageID, 1)
	if err != nil || works.ExecutionID != id || works.ImageID != first.Items[0].ImageID || works.PageSize != 50 || works.Total != 51 || len(works.Items) != 50 || works.Items[0].WorkID != "9223372036854775807" {
		t.Fatal(works, err)
	}
	for _, dto := range []any{first, last, works} {
		data, err := json.Marshal(dto)
		if err != nil || strings.Contains(string(data), private) || strings.Contains(string(data), `"path"`) || strings.Contains(string(data), `"err"`) {
			t.Fatal("private fields leaked", string(data), err)
		}
	}
	first.Items[0].Reason = "mutated"
	works.Items[0].Title = "mutated"
	if again, _ := a.GetBackupOmittedImageWorks(id, works.ImageID, 1); again.Items[0].Title == "mutated" {
		t.Fatal("DTO aliases retained result")
	}
	for _, page := range []int{-1, 0, 3, int(^uint(0) >> 1)} {
		if _, err := a.GetBackupOmittedImages(id, page); errorCode(t, err) != "invalid_input" {
			t.Fatal(page, err)
		}
	}
	if _, err := a.GetBackupOmittedImageWorks(id, "invalid", 1); errorCode(t, err) != "invalid_input" {
		t.Fatal(err)
	}
	if _, err := a.GetBackupOmittedImageWorks(id, strings.Repeat("f", 32), 1); errorCode(t, err) != "backup_image_not_found" {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, a.backupRuns.snapshot()) {
		t.Fatal("observation changed retained state")
	}
}

func TestBackupAPIDetailAvailability(t *testing.T) {
	for _, state := range []backupWorkNamesState{backupWorkNamesUnobserved, backupWorkNamesUnavailable, backupWorkNamesAvailable} {
		var a App
		r := backupDetailsFixture(1, 0)
		r.Omissions.WorkNamesState = state
		id := retainBackupAPIResult(t, &a, r)
		got, err := a.GetBackupOmittedImageWorks(id, r.Omissions.Images[0].ID, 1)
		if state == backupWorkNamesAvailable {
			data, jsonErr := json.Marshal(got)
			if err != nil || jsonErr != nil || got.Items == nil || !strings.Contains(string(data), `"items":[]`) {
				t.Fatal(got, err, jsonErr)
			}
		} else {
			want := "backup_details_unobserved"
			if state == backupWorkNamesUnavailable {
				want = "backup_work_names_unavailable"
			}
			if errorCode(t, err) != want {
				t.Fatal(err)
			}
		}
	}
	var a App
	id := retainBackupAPIResult(t, &a, backupSaveRunResult{})
	if _, err := a.GetBackupOmittedImages(id, 1); errorCode(t, err) != "backup_details_unobserved" {
		t.Fatal(err)
	}
	for _, cause := range []error{errBackupDetailRequest, errBackupDetailStale, errBackupDetailPending, errBackupDetailUnobserved, errBackupDetailUnavailable, errBackupDetailImage, errors.New("unknown")} {
		err := backupDetailAPIError(errors.Join(cause, errors.New("private-diagnostic")))
		wire, _ := formatAPIError(err).(string)
		if strings.Contains(wire, "private-diagnostic") {
			t.Fatal("diagnostic leaked", wire)
		}
	}
}
