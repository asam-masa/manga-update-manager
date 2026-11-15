package main

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/platform"
)

func TestBackupStatusAPILifetime(t *testing.T) {
	var a App
	var ticks atomic.Int64
	base := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	a.backupRuns.now = func() time.Time { return base.Add(time.Duration(ticks.Load())) }
	idle := a.GetBackupStatus()
	data, _ := json.Marshal(idle)
	if idle.State != "idle" || idle.ExecutionID != "" || idle.Stage != "unobserved" || idle.Result != nil || idle.CanCancel || string(data) != `{"executionId":"","state":"idle","stage":"unobserved","elapsedSeconds":0,"canCancel":false,"result":null}` {
		t.Fatal(idle, string(data))
	}
	op, err := a.operationGate.BeginBackup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer op.End()
	release, finish := holdBackupRun(t)
	run, err := a.backupRuns.start(context.Background(), "private-path", func(context.Context, application.BackupExecutionID) (backupSaveRunResult, error) {
		<-release
		return backupDetailsFixture(0, 0), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ticks.Store(int64(3500 * time.Millisecond))
	before := a.backupRuns.snapshot()
	view := a.GetBackupStatus()
	if !reflect.DeepEqual(before, a.backupRuns.snapshot()) {
		t.Fatal("live observation mutated run")
	}
	if view.State != "running" || view.ExecutionID == "" || view.ElapsedSeconds != 3 || !view.CanCancel || view.Stage != "unobserved" || view.Result != nil {
		t.Fatal(view)
	}
	ticks.Store(int64(^uint64(0) >> 1))
	if long := a.GetBackupStatus(); long.ElapsedSeconds != 9223372036 {
		t.Fatal("elapsed precision", long)
	}
	ticks.Store(-int64(time.Second))
	if backwards := a.GetBackupStatus(); backwards.ElapsedSeconds != 0 {
		t.Fatal("negative elapsed", backwards)
	}
	ticks.Store(int64(3500 * time.Millisecond))
	if !a.backupRuns.reportStage(run.id, backupStagePublishing) {
		t.Fatal("stage refused")
	}
	if view := a.GetBackupStatus(); view.State != "running" || view.Stage != "publishing" || !view.CanCancel {
		t.Fatal("label changed permission", view)
	}
	if _, err := a.CancelBackup(view.ExecutionID); err != nil {
		t.Fatal(err)
	}
	if view := a.GetBackupStatus(); view.State != "cancelling" || view.Result != nil || view.CanCancel {
		t.Fatal(view)
	}
	finish()
	waitBackupRun(t, run.done)
	op.EndWithResume(false)
	finished := a.GetBackupStatus()
	if finished.State != "finished" || finished.Result == nil || finished.CanCancel || finished.Result.Publication != "not_attempted" || finished.Result.HasWarnings || finished.Result.OmittedImageCount == nil || *finished.Result.OmittedImageCount != 0 {
		t.Fatal(finished)
	}
	ticks.Store(int64(100 * time.Second))
	if !reflect.DeepEqual(finished, a.GetBackupStatus()) {
		t.Fatal("completed elapsed moved")
	}
	*finished.Result.OmittedImageCount = 999
	if next := a.GetBackupStatus(); *next.Result.OmittedImageCount != 0 {
		t.Fatal("DTO aliases retained count", next)
	}
	before = a.backupRuns.snapshot()
	_ = a.GetBackupStatus()
	if !reflect.DeepEqual(before, a.backupRuns.snapshot()) {
		t.Fatal("observation mutated run")
	}
	secondRelease, secondFinish := holdBackupRun(t)
	second, err := a.backupRuns.start(context.Background(), "next-private-path", func(context.Context, application.BackupExecutionID) (backupSaveRunResult, error) {
		<-secondRelease
		return backupSaveRunResult{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if next := a.GetBackupStatus(); next.ExecutionID == view.ExecutionID || next.Result != nil || next.ElapsedSeconds != 0 {
		t.Fatal("next generation mixed", next)
	}
	if !a.backupRuns.beginPublication(second.id) {
		t.Fatal("publication refused")
	}
	if next := a.GetBackupStatus(); next.State != "publishing" || next.CanCancel {
		t.Fatal(next)
	}
	secondFinish()
	waitBackupRun(t, second.done)
	a.backupRuns.close()
	if !reflect.DeepEqual(idle, a.GetBackupStatus()) {
		t.Fatal("closed owner retained status")
	}
}

func TestBackupStatusAPIResultObservations(t *testing.T) {
	for _, state := range []platform.BackupPublicationState{platform.BackupPublishNotAttempted, platform.BackupPublishCollision, platform.BackupPublishConfirmed, platform.BackupPublishUnknown, platform.BackupPublicationState(255)} {
		var a App
		r := backupDetailsFixture(1, 1)
		r.Save.Publication.State = state
		r.Save.Publication.TemporaryPath = "private-path"
		r.CreationError = errors.New("private-diagnostic")
		id := retainBackupAPIResult(t, &a, r)
		got := a.GetBackupStatus()
		want := map[platform.BackupPublicationState]string{platform.BackupPublishNotAttempted: "not_attempted", platform.BackupPublishCollision: "collision", platform.BackupPublishConfirmed: "confirmed"}[state]
		if want == "" {
			want = "unknown"
		}
		if got.ExecutionID != id || got.Result == nil || got.Result.Publication != want || !got.Result.HasWarnings || *got.Result.OmittedImageCount != 1 || *got.Result.WorkCount != 1 {
			t.Fatal(got)
		}
		data, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		// No fields carry diagnostic values or approved destination paths.
		if contains := string(data); strings.Contains(contains, "private-path") || strings.Contains(contains, "private-diagnostic") {
			t.Fatal(contains)
		}
	}
	for _, availability := range []backupWorkNamesState{backupWorkNamesUnobserved, backupWorkNamesUnavailable, backupWorkNamesAvailable} {
		var a App
		r := backupSaveRunResult{}
		r.Omissions.WorkNamesState = availability
		_ = retainBackupAPIResult(t, &a, r)
		got := a.GetBackupStatus().Result
		if got.OmittedImageCount != nil || (got.WorkCount != nil) != (availability == backupWorkNamesAvailable) {
			t.Fatal(got)
		}
		data, err := json.Marshal(got)
		if err != nil || !strings.Contains(string(data), `"omittedImageCount":null`) {
			t.Fatal(string(data), err)
		}
	}
}
