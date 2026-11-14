package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
)

func TestBeginBackupReservesThroughStartupCleanup(t *testing.T) {
	dir := t.TempDir()
	a := testApp(t, dir)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(release) }) }
	t.Cleanup(finish)
	a.covers = application.NewCoverImporter(cleanupListBlock{a.store, entered, release}, &a.coverOperations, dir, nil, time.Now)
	notices := 0
	a.backupBusyNotice = func(context.Context) { notices++ }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		op, _, err := a.beginBackup(ctx)
		if op != nil {
			op.End()
			t.Error("cancelled startup cleanup returned a backup ticket")
		}
		done <- err
	}()
	waitBackupRun(t, entered)
	// Cleanup itself enters as normal. Reserving backup must not pause it.
	end, err := a.operationGate.BeginNormal()
	if err != nil {
		t.Fatal("normal entry paused before cleanup returned", err)
	}
	end()
	if !a.beforeClose(context.Background()) || notices != 1 {
		t.Fatal("cleanup did not retain backup close notice", notices)
	}
	if op, _, err := a.beginBackup(context.Background()); op != nil || !errors.Is(err, application.ErrBackupBusy) {
		t.Fatal("duplicate entered startup cleanup", op, err)
	}
	cancel()
	select {
	case err := <-done:
		t.Fatal("cancel abandoned running cleanup", err)
	default:
	}
	if _, err := a.operationGate.RequestClose(nil); !errors.Is(err, application.ErrBackupBusy) {
		t.Fatal("cancel allowed close before release", err)
	}
	finish()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup did not release reservation")
	}
	if a.beforeClose(context.Background()) {
		t.Fatal("finished cleanup retained backup reservation")
	}
}

func TestBeginBackupWarningReturnsReservation(t *testing.T) {
	a := testApp(t, t.TempDir())
	if err := a.store.ReserveManagedCover(context.Background(), strings.Repeat("a", 32)); err != nil {
		t.Fatal(err)
	}
	op, warnings, err := a.beginBackup(context.Background())
	if op != nil || !errors.Is(err, errBackupCleanupUnverified) || len(warnings) != 1 || warnings[0].Code != "cover_cleanup_unverified" {
		t.Fatal(op, warnings, err)
	}
	end, err := a.operationGate.BeginNormal()
	if err != nil {
		t.Fatal("warning leaked reservation", err)
	}
	end()
	if a.beforeClose(context.Background()) {
		t.Fatal("warning prevented close")
	}
}

func TestBeginBackupBeforeStartupDoesNotReserveClose(t *testing.T) {
	a := NewApp()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observed := &startupWaitContext{Context: ctx, entered: make(chan struct{})}
	done := make(chan error, 1)
	go func() { _, _, err := a.beginBackup(observed); done <- err }()
	waitBackupRun(t, observed.entered)
	if a.beforeClose(context.Background()) {
		t.Fatal("pre-startup wait prevented close")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pre-startup wait did not cancel")
	}
	a.shutdown(context.Background())
}
