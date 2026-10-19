package main

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

type cleanupListBlock struct {
	application.CoverRepository
	entered chan struct{}
	release chan struct{}
}

func (r cleanupListBlock) ListManagedCovers(ctx context.Context) ([]storage.ManagedCover, error) {
	close(r.entered)
	select {
	case <-r.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return r.CoverRepository.ListManagedCovers(ctx)
}

func TestStartupCleanupWaitsJoinsAndKeepsDatabaseUntilRelease(t *testing.T) {
	app := NewApp()
	dir := t.TempDir()
	app.dataDirectory = func() (string, error) { return dir, nil }
	app.startup(context.Background())
	defer app.shutdown(context.Background())
	if _, err := app.SelectWorkCover(1); errorCode(t, err) != "cover_busy" {
		t.Fatal(err)
	}
	app.coverBusyNotice = func(context.Context) {}
	if err := app.store.ReserveManagedCover(context.Background(), strings.Repeat("a", 32)); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	app.covers = application.NewCoverImporter(cleanupListBlock{app.store, entered, release}, &app.coverOperations, dir, nil, time.Now)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		warnings, err := app.LoadCoverWarnings()
		if err != nil || len(warnings) != 1 || warnings[0].Code != "cover_cleanup_unverified" {
			t.Error(warnings, err)
		}
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("cleanup not started")
	}
	if !app.beforeClose(context.Background()) {
		t.Fatal("running cleanup allowed close")
	}
	if _, err := app.ListWorks(); err != nil {
		t.Fatal("cleanup prevented normal DB read", err)
	}
	closed := make(chan struct{})
	go func() { app.shutdown(context.Background()); close(closed) }()
	select {
	case <-closed:
		t.Fatal("DB closed during cleanup")
	default:
	}
	close(release)
	wg.Wait()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown deadlock")
	}
	app.startup(context.Background())
	if app.store != nil {
		t.Fatal("delayed startup reopened DB")
	}
}

func TestStartupCleanupWarningsAreRetainedCopiesAndSafe(t *testing.T) {
	app := testApp(t, t.TempDir())
	if err := app.store.ReserveManagedCover(context.Background(), strings.Repeat("b", 32)); err != nil {
		t.Fatal(err)
	}
	got, err := app.LoadCoverWarnings()
	if err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
	got[0].Message = "caller mutation"
	again, err := app.LoadCoverWarnings()
	if err != nil || len(again) != 1 || again[0].Message == "caller mutation" {
		t.Fatal(again, err)
	}
	data, _ := json.Marshal(again)
	if strings.Contains(string(data), "records.sqlite") || strings.Contains(string(data), strings.Repeat("b", 32)) {
		t.Fatal(string(data))
	}
	if app.beforeClose(context.Background()) {
		t.Fatal("completed cleanup prevents close")
	}
}

func TestStartupCleanupConcurrentCallsJoin(t *testing.T) {
	app := testApp(t, t.TempDir())
	entered, release := make(chan struct{}), make(chan struct{})
	app.covers = application.NewCoverImporter(cleanupListBlock{app.store, entered, release}, &app.coverOperations, t.TempDir(), nil, time.Now)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			warnings, err := app.LoadCoverWarnings()
			if err != nil || warnings == nil || len(warnings) != 0 {
				t.Error(warnings, err)
			}
		}()
	}
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("not started")
	}
	close(release)
	wg.Wait()
	warnings, err := app.LoadCoverWarnings()
	if err != nil || warnings == nil || len(warnings) != 0 {
		t.Fatal(warnings, err)
	}
}

func TestStartupCleanupDatabaseStopWarning(t *testing.T) {
	app := testApp(t, t.TempDir())
	if err := app.store.Close(); err != nil {
		t.Fatal(err)
	}
	warnings, err := app.LoadCoverWarnings()
	if err != nil || len(warnings) != 1 || warnings[0].Code != "database_stopped" {
		t.Fatal(warnings, err)
	}
	if _, err := app.SelectWorkCover(1); errorCode(t, err) != "cover_unavailable" {
		t.Fatal(err)
	}
}

func TestCloseBeforeCleanupStartsDoesNotTrapWindow(t *testing.T) {
	app := testApp(t, t.TempDir())
	app.coverBusyNotice = func(context.Context) { t.Fatal("pending cleanup reported as running") }
	if app.beforeClose(context.Background()) {
		t.Fatal("pending-only cleanup trapped close")
	}
	// A late frontend request must not start cleanup after close was accepted.
	entered, release := make(chan struct{}), make(chan struct{})
	close(release)
	app.covers = application.NewCoverImporter(cleanupListBlock{app.store, entered, release}, &app.coverOperations, t.TempDir(), nil, time.Now)
	if _, err := app.LoadCoverWarnings(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
		t.Fatal("late request started cleanup after close")
	default:
	}
}

func TestLoadCoverWarningsWaitsForStartupAndShutdown(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		app := NewApp()
		app.dataDirectory = func() (string, error) { return t.TempDir(), nil }
		done := make(chan error, 1)
		go func() { _, err := app.LoadCoverWarnings(); done <- err }()
		select {
		case <-done:
			t.Fatal("load did not wait for startup")
		default:
		}
		if shutdown {
			app.shutdown(context.Background())
		} else {
			app.startup(context.Background())
		}
		select {
		case err := <-done:
			if (err != nil) != shutdown {
				t.Fatal(err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("load stuck")
		}
		app.shutdown(context.Background())
	}
}
