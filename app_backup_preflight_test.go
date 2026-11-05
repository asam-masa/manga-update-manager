package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
)

func TestBackupCleanupPreflightStartupCancellation(t *testing.T) {
	a := NewApp()
	ctx, cancel := context.WithCancel(context.Background())
	observed := &startupWaitContext{Context: ctx, entered: make(chan struct{})}
	done := make(chan error, 1)
	go func() { _, err := a.prepareBackupCleanup(observed); done <- err }()
	<-observed.entered
	if !a.mu.TryLock() {
		t.Fatal("startup wait holds App lock")
	}
	a.mu.Unlock()
	if a.beforeClose(context.Background()) {
		t.Fatal("startup wait prevents close")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("startup wait did not cancel")
	}
	a.shutdown(context.Background())
}

func TestBackupCleanupPreflightWarningsRetained(t *testing.T) {
	a := testApp(t, t.TempDir())
	if err := a.store.ReserveManagedCover(context.Background(), strings.Repeat("a", 32)); err != nil {
		t.Fatal(err)
	}
	w, err := a.prepareBackupCleanup(context.Background())
	if !errors.Is(err, errBackupCleanupUnverified) || len(w) != 1 || w[0].Code != "cover_cleanup_unverified" {
		t.Fatal(w, err)
	}
	w[0].Code = "mutated"
	w, err = a.LoadCoverWarnings()
	if err != nil || len(w) != 1 || w[0].Code != "cover_cleanup_unverified" {
		t.Fatal("warnings lost", w, err)
	}
	end, err := a.operationGate.BeginNormal()
	if err != nil {
		t.Fatal("preflight reserved backup", err)
	}
	end()
}

func TestBackupCleanupPreflightJoinsAndDoesNotAbandonCleanup(t *testing.T) {
	dir := t.TempDir()
	a := testApp(t, dir)
	entered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	a.covers = application.NewCoverImporter(cleanupListBlock{a.store, entered, release}, &a.coverOperations, dir, nil, time.Now)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := a.prepareBackupCleanup(ctx); done <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup not started")
	}
	joined := make(chan error, 1)
	go func() { _, err := a.LoadCoverWarnings(); joined <- err }()
	cancel()
	select {
	case err := <-done:
		t.Fatal("abandoned running cleanup", err)
	default:
	}
	close(release)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup did not complete")
	}
	if err := <-joined; err != nil {
		t.Fatal(err)
	}
	if _, err := a.prepareBackupCleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(a.LoadCoverProgress()) != 0 {
		t.Fatal("progress retained")
	}
	op, err := a.coverOperations.BeginRead()
	if err != nil {
		t.Fatal("cleanup ticket retained", err)
	}
	if err := op.End(); err != nil {
		t.Fatal(err)
	}
}

func TestBackupCleanupPreflightUnavailable(t *testing.T) {
	t.Run("startup failure", func(t *testing.T) {
		a := NewApp()
		a.dataDirectory = func() (string, error) { return "", errors.New("synthetic failure") }
		a.startup(context.Background())
		if _, err := a.prepareBackupCleanup(context.Background()); errorCode(t, err) != "database_unavailable" {
			t.Fatal(err)
		}
		a.shutdown(context.Background())
	})
	t.Run("close before cleanup", func(t *testing.T) {
		a := testApp(t, t.TempDir())
		if a.beforeClose(context.Background()) {
			t.Fatal("close refused")
		}
		if _, err := a.prepareBackupCleanup(context.Background()); !errors.Is(err, errBackupCleanupUnverified) {
			t.Fatal(err)
		}
	})
}
