package main

import (
	"context"
	"errors"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/application"
)

func TestBackupSelectionCloseDoesNotStackNotice(t *testing.T) {
	a := testApp(t, t.TempDir())
	if _, err := a.LoadCoverWarnings(); err != nil {
		t.Fatal(err)
	}
	a.backupBusyNotice = func(context.Context) { t.Error("notice stacked over picker") }
	entered := make(chan struct{})
	release, finish := holdBackupRun(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		run, err := a.selectBackupRun(func(context.Context) (string, error) { close(entered); <-release; return "", nil }, syntheticBackupSuccess)
		if err != nil || run.id != 0 {
			t.Error("cancel started backup", run, err)
		}
	}()
	waitBackupRun(t, entered)
	for i := 0; i < 3; i++ {
		if !a.beforeClose(context.Background()) {
			t.Fatal("selection allowed close")
		}
	}
	finish()
	waitBackupRun(t, done)
	_, err := a.fileSelections.choose(context.Background(), func(context.Context) (string, error) { return "", nil })
	if err != nil {
		t.Fatal("selection gate leaked", err)
	}
	if a.beforeClose(context.Background()) {
		t.Fatal("cancelled selection still prevents close")
	}
}

func TestBackupCloseNoticePreventsRacingPicker(t *testing.T) {
	a := testApp(t, t.TempDir())
	if _, err := a.LoadCoverWarnings(); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release, finish := holdBackupRun(t)
	run, err := a.startBackupRun("synthetic", func(context.Context, application.BackupExecutionID) (backupSaveRunResult, bool, error) {
		close(entered)
		<-release
		return backupSaveRunResult{}, true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	waitBackupRun(t, entered)
	called := false
	a.backupBusyNotice = func(ctx context.Context) {
		called = true
		_, err := a.fileSelections.choose(ctx, func(context.Context) (string, error) { t.Error("picker raced notice"); return "", nil })
		if !errors.Is(err, errFileSelectionBusy) {
			t.Error(err)
		}
	}
	if !a.beforeClose(context.Background()) || !called {
		t.Fatal("processing notice lost")
	}
	_, err = a.fileSelections.choose(context.Background(), func(context.Context) (string, error) { return "", nil })
	if err != nil {
		t.Fatal("notice gate leaked", err)
	}
	finish()
	waitBackupRun(t, run.done)
	if a.beforeClose(context.Background()) {
		t.Fatal("completed backup prevents close")
	}
}
