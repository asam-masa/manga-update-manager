package main

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/application"
)

func TestBackupSelectionAdmissionAndHandoff(t *testing.T) {
	a := testApp(t, t.TempDir())
	a.backupBusyNotice = func(context.Context) {}
	if _, err := a.LoadCoverWarnings(); err != nil {
		t.Fatal(err)
	}
	w, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/picker", Title: "Synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	a.chooseCover = func(context.Context) (string, error) { t.Error("second picker opened"); return "", nil }
	previous, err := a.startBackupRun("previous.zip", syntheticBackupSuccess)
	if err != nil {
		t.Fatal(err)
	}
	waitBackupRun(t, previous.done)
	before := a.backupRuns.snapshot()
	entered := make(chan struct{})
	release, finish := holdBackupRun(t)
	returned := make(chan struct{})
	var run backupRun
	var selectErr error
	workerEntered := make(chan struct{})
	workerRelease, workerFinish := holdBackupRun(t)
	go func() {
		defer close(returned)
		run, selectErr = a.selectBackupRun(func(context.Context) (string, error) {
			close(entered)
			<-release
			return "selected.zip", nil
		}, func(context.Context, application.BackupExecutionID) (backupSaveRunResult, bool, error) {
			close(workerEntered)
			<-workerRelease
			return backupSaveRunResult{}, true, nil
		})
	}()
	waitBackupRun(t, entered)
	if !reflect.DeepEqual(before, a.backupRuns.snapshot()) {
		t.Fatal("selection erased prior result")
	}
	if !a.beforeClose(context.Background()) {
		t.Fatal("selection allowed close")
	}
	if _, err := a.startBackupRun("duplicate.zip", syntheticBackupSuccess); !errors.Is(err, application.ErrBackupBusy) {
		t.Fatal(err)
	}
	if _, err := a.SelectWorkCover(w.ID); errorCode(t, err) != "file_selection_busy" {
		t.Fatal(err)
	}
	// Normal work is admitted during selection and drains only after acceptance.
	end, err := a.operationGate.BeginNormal()
	if err != nil {
		t.Fatal(err)
	}
	defer end()
	if _, err := a.ListWorks(); err != nil {
		t.Fatal(err)
	}
	finish()
	waitBackupRun(t, returned)
	if selectErr != nil || run.id == previous.id {
		t.Fatal(run, selectErr)
	}
	select {
	case <-workerEntered:
		t.Fatal("worker passed active normal operation")
	default:
	}
	if !a.beforeClose(context.Background()) {
		t.Fatal("handoff released reservation")
	}
	end()
	waitBackupRun(t, workerEntered)
	workerFinish()
	waitBackupRun(t, run.done)
	if got := a.backupRuns.snapshot(); got.destination != "selected.zip" || got.result == nil {
		t.Fatal(got)
	}
}

func TestCoverSelectionRejectsBackupWithoutQueue(t *testing.T) {
	a := testApp(t, t.TempDir())
	if _, err := a.LoadCoverWarnings(); err != nil {
		t.Fatal(err)
	}
	w, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/reverse", Title: "Synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release, finish := holdBackupRun(t)
	a.chooseCover = func(context.Context) (string, error) { close(entered); <-release; return "", nil }
	done := make(chan struct{})
	var coverErr error
	go func() { defer close(done); _, coverErr = a.SelectWorkCover(w.ID) }()
	waitBackupRun(t, entered)
	if _, err := a.selectBackupRun(func(context.Context) (string, error) {
		t.Error("queued/second picker opened")
		return "unexpected.zip", nil
	}, syntheticBackupSuccess); !errors.Is(err, errFileSelectionBusy) {
		t.Fatal(err)
	}
	finish()
	waitBackupRun(t, done)
	if coverErr != nil {
		t.Fatal(coverErr)
	}
	// Rejected reservation and picker were released; no queued callback runs.
	run, err := a.selectBackupRun(func(context.Context) (string, error) { return "retry.zip", nil }, syntheticBackupSuccess)
	if err != nil {
		t.Fatal(err)
	}
	waitBackupRun(t, run.done)
}

func TestBackupSelectionFailuresKeepResult(t *testing.T) {
	for _, kind := range []string{"cancel", "picker error", "stop after selection", "context cancelled"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t, t.TempDir())
			run, err := a.startBackupRun("previous.zip", syntheticBackupSuccess)
			if err != nil {
				t.Fatal(err)
			}
			waitBackupRun(t, run.done)
			before := a.backupRuns.snapshot()
			pickerErr := errors.New("synthetic picker failure")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			a.ctx = ctx
			run, err = a.selectBackupRun(func(context.Context) (string, error) {
				switch kind {
				case "cancel":
					return "", nil
				case "picker error":
					return "", pickerErr
				case "stop after selection":
					a.coverOperations.Stop()
				case "context cancelled":
					cancel()
				}
				return "rejected.zip", nil
			}, func(context.Context, application.BackupExecutionID) (backupSaveRunResult, bool, error) {
				t.Error("rejected worker started")
				return backupSaveRunResult{}, true, nil
			})
			if run.id != 0 || !reflect.DeepEqual(before, a.backupRuns.snapshot()) {
				t.Fatal("previous result lost", err)
			}
			if kind == "cancel" && err != nil || kind != "cancel" && err == nil {
				t.Fatal(err)
			}
			if kind == "picker error" && !errors.Is(err, pickerErr) {
				t.Fatal(err)
			}
			// Inspect release directly: stop/cancel must not mask a leaked ticket.
			op, reserveErr := a.operationGate.ReserveBackup(context.Background())
			if reserveErr != nil {
				t.Fatal("reservation leaked", reserveErr)
			}
			op.End()
			if _, err := a.fileSelections.choose(context.Background(), func(context.Context) (string, error) { return "", nil }); err != nil {
				t.Fatal("picker leaked", err)
			}
		})
	}
}
