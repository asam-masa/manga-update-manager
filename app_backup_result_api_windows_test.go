package main

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/platform"
)

func TestBackupResultAPIClassification(t *testing.T) {
	for _, mode := range []string{"saved", "omitted", "postprocess", "not-saved", "cancel-and-error", "collision", "unknown", "future", "unconfirmed", "finish-error", "details", "cleanup"} {
		t.Run(mode, func(t *testing.T) {
			var a App
			r := backupDetailsFixture(0, 0)
			r.RetrySafetyConfirmed = true
			r.Save.Publication.State = platform.BackupPublishConfirmed
			cause := errors.New("private-diagnostic")
			want, retry, notice := "saved", true, ""
			switch mode {
			case "omitted":
				r.Omissions = backupDetailsFixture(1, 1).Omissions
				want, notice = "saved_with_warnings", "images_omitted"
			case "postprocess":
				want, notice = "saved_with_warnings", "postprocess_warning"
			case "not-saved":
				r.Save.Publication.State = platform.BackupPublishNotAttempted
				want, notice = "not_saved", "processing_incomplete"
			case "cancel-and-error":
				r.Save.Publication.State = platform.BackupPublishNotAttempted
				r.FinishError = errors.Join(cause, context.Canceled)
				want, retry, notice = "not_saved", false, "cancellation_observed"
			case "collision":
				r.Save.Publication.State = platform.BackupPublishCollision
				want = "collision"
			case "unknown":
				r.Save.Publication.State = platform.BackupPublishUnknown
				want, retry = "unknown", false
			case "future":
				r.Save.Publication.State = platform.BackupPublicationState(255)
				want, retry = "unknown", false
			case "unconfirmed":
				r.RetrySafetyConfirmed = false
				want, retry = "saved_with_warnings", false
			case "finish-error":
				r.FinishError = errBackupReleaseUnverified
				want, retry = "saved_with_warnings", false
			case "details":
				r.Omissions.WorkNamesState = backupWorkNamesUnavailable
				r.Omissions.DetailError = cause
				want, notice = "saved_with_warnings", "work_names_unavailable"
			case "cleanup":
				r.Database.CleanupError = cause
				r.Save.Cleanup = &platform.BackupDiscardResult{TemporaryPath: "private-path"}
				r.Warnings = []CoverWarningDTO{{Code: "private-code", Message: "private-warning"}}
				want, notice = "saved_with_warnings", "cleanup_warning"
			}
			resultErr := r.FinishError
			if mode == "postprocess" {
				resultErr = cause
			}
			run, err := a.backupRuns.start(context.Background(), "private-path", func(context.Context, application.BackupExecutionID) (backupSaveRunResult, error) { return r, resultErr })
			if err != nil {
				t.Fatal(err)
			}
			waitBackupRun(t, run.done)
			id := strconv.FormatUint(uint64(run.id), 10)
			before := a.backupRuns.snapshot()
			got, err := a.GetBackupResult(id)
			if err != nil || got.ExecutionID != id || got.Outcome != want || got.RetryAllowed != retry || got.Notices == nil {
				t.Fatal(got, err)
			}
			if got.CancellationObserved != (mode == "cancel-and-error") {
				t.Fatal(got)
			}
			found := notice == ""
			for _, n := range got.Notices {
				if n.Code == notice {
					found = true
				}
			}
			if !found {
				t.Fatal("missing notice", notice, got)
			}
			data, err := json.Marshal(got)
			if err != nil || strings.Contains(string(data), "private-") {
				t.Fatal("private data leaked", string(data), err)
			}
			if len(got.Notices) > 0 {
				got.Notices[0].Message = "mutated"
			}
			if !reflect.DeepEqual(before, a.backupRuns.snapshot()) {
				t.Fatal("result read changed state")
			}
		})
	}
}

func TestBackupResultAPIErrorsAndSafetyObservation(t *testing.T) {
	var a App
	if _, err := a.GetBackupResult("01"); errorCode(t, err) != "invalid_input" {
		t.Fatal(err)
	}
	if _, err := a.GetBackupResult("1"); errorCode(t, err) != "backup_result_expired" {
		t.Fatal(err)
	}
	release, finish := holdBackupRun(t)
	run, err := a.backupRuns.start(context.Background(), "private", func(context.Context, application.BackupExecutionID) (backupSaveRunResult, error) {
		<-release
		return backupSaveRunResult{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.GetBackupResult("1"); errorCode(t, err) != "backup_result_pending" {
		t.Fatal(err)
	}
	finish()
	waitBackupRun(t, run.done)
	got, err := a.GetBackupResult("1")
	if err != nil || got.RetryAllowed {
		t.Fatal("missing safety observation allowed retry", got, err)
	}
	_ = retainBackupAPIResult(t, &a, backupSaveRunResult{})
	if _, err := a.GetBackupResult("1"); errorCode(t, err) != "backup_result_expired" {
		t.Fatal(err)
	}
}

func TestAppBackupResultRecordsFinalSafety(t *testing.T) {
	for _, released := range []bool{true, false} {
		a := testApp(t, t.TempDir())
		run, err := a.startBackupRun("synthetic", func(context.Context, application.BackupExecutionID) (backupSaveRunResult, bool, error) {
			return backupSaveRunResult{}, released, context.Canceled
		})
		if err != nil {
			t.Fatal(err)
		}
		waitBackupRun(t, run.done)
		got := a.backupRuns.snapshot().result
		if got == nil || got.save.RetrySafetyConfirmed != released {
			t.Fatal("safety not retained", got)
		}
		api, err := a.GetBackupResult("1")
		if err != nil || api.RetryAllowed != released || !api.CancellationObserved {
			t.Fatal(api, err)
		}
		if !released && !errors.Is(got.err, errBackupReleaseUnverified) {
			t.Fatal(got.err)
		}
	}
}

func TestBackupResultPreflightSafety(t *testing.T) {
	for _, mode := range []string{"request-cancel", "preparation-warning", "app-cancel"} {
		a := testApp(t, t.TempDir())
		appContext, cancelApp := context.WithCancel(context.Background())
		defer cancelApp()
		a.mu.Lock()
		a.ctx = appContext
		a.mu.Unlock()
		if _, err := a.LoadCoverWarnings(); err != nil {
			t.Fatal(err)
		}
		if mode == "preparation-warning" {
			a.mu.Lock()
			a.coverCleanupWarnings = []CoverWarningDTO{{Code: "synthetic", Message: "private-warning"}}
			a.mu.Unlock()
		}
		end, err := a.operationGate.BeginNormal()
		if err != nil {
			t.Fatal(err)
		}
		defer end()
		run, err := a.startBackupRun("synthetic", func(context.Context, application.BackupExecutionID) (backupSaveRunResult, bool, error) {
			t.Error("worker passed preflight refusal/cancellation")
			return backupSaveRunResult{}, true, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if mode == "app-cancel" {
			cancelApp()
		}
		if mode == "request-cancel" && a.backupRuns.cancel(run.id) != application.BackupCancelAccepted {
			t.Fatal("cancel refused")
		}
		waitBackupRun(t, run.done)
		got, err := a.GetBackupResult(strconv.FormatUint(uint64(run.id), 10))
		if err != nil || got.RetryAllowed != (mode == "request-cancel") || got.Outcome != "not_saved" {
			t.Fatal(got, err)
		}
		if mode == "preparation-warning" {
			data, _ := json.Marshal(got)
			if strings.Contains(string(data), "private-warning") {
				t.Fatal("warning leaked", string(data))
			}
		}
	}
}
