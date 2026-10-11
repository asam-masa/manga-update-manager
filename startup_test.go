package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/platform"
)

type closeFunc func() error

func (f closeFunc) Close() error { return f() }

func TestStartupGateDoesNotStartDesktopOrDatabaseOnFailure(t *testing.T) {
	for _, failure := range []string{"directory", "busy", "lock"} {
		t.Run(failure, func(t *testing.T) {
			app := NewApp()
			directory := t.TempDir()
			app.dataDirectory = func() (string, error) {
				if failure == "directory" {
					return "", errors.New("private directory diagnostic")
				}
				return directory, nil
			}
			acquired := false
			err := runApplication(app, func(string) (io.Closer, error) {
				acquired = true
				if failure == "busy" {
					return nil, platform.ErrStorageInUse
				}
				return nil, errors.New("private lock diagnostic")
			}, func(*App) error {
				t.Fatal("desktop started without a storage lock")
				return nil
			})
			if err == nil || acquired != (failure != "directory") || app.store != nil {
				t.Fatalf("gate result: %v, acquired=%v", err, acquired)
			}
			if _, err := os.Stat(filepath.Join(directory, "manga.sqlite")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("database touched: %v", err)
			}
			if strings.Contains(startupErrorMessage(err), "private") {
				t.Fatal("diagnostic exposed")
			}
		})
	}
}

func TestStartupGateKeepsLockThroughDatabaseShutdown(t *testing.T) {
	for _, result := range []string{"success", "run_error", "panic"} {
		t.Run(result, func(t *testing.T) {
			app := NewApp()
			directory := t.TempDir()
			resolutions := 0
			app.dataDirectory = func() (string, error) { resolutions++; return directory, nil }
			held, closed := false, false
			func() {
				defer func() {
					if got := recover(); (got != nil) != (result == "panic") {
						t.Fatalf("panic = %v", got)
					}
				}()
				err := runApplication(app, func(got string) (io.Closer, error) {
					if got != directory {
						t.Fatal("wrong storage")
					}
					held = true
					return closeFunc(func() error {
						if app.store != nil {
							t.Fatal("lock released before database close")
						}
						closed, held = true, false
						return nil
					}), nil
				}, func(app *App) error {
					if !held {
						t.Fatal("lock not held")
					}
					app.startup(context.Background())
					if app.store == nil {
						t.Fatal("DB initialization failed")
					}
					if result == "panic" {
						panic("synthetic run failure")
					}
					if result == "run_error" {
						return errors.New("synthetic run failure")
					}
					app.shutdown(context.Background())
					return nil
				})
				if (err != nil) != (result == "run_error") {
					t.Fatalf("run result: %v", err)
				}
			}()
			if held || !closed || resolutions != 1 {
				t.Fatalf("held=%v closed=%v resolutions=%d", held, closed, resolutions)
			}
		})
	}
}

func TestStartupMessages(t *testing.T) {
	if !strings.Contains(startupErrorMessage(platform.ErrStorageInUse), "既に起動") {
		t.Fatal("busy message missing")
	}
	if !strings.Contains(startupErrorMessage(errors.New("private diagnostic")), "安全に起動") {
		t.Fatal("failure message missing")
	}
}

func TestDelayedStartupCannotReopenAfterLockRelease(t *testing.T) {
	app := NewApp()
	directory := t.TempDir()
	app.dataDirectory = func() (string, error) { return directory, nil }
	var delayed func(context.Context)
	released := false
	err := runApplication(app, func(string) (io.Closer, error) {
		return closeFunc(func() error { released = true; return nil }), nil
	}, func(app *App) error {
		// Reproduce a Wails callback queued until after Run has returned.
		delayed = app.startup
		app.shutdown(context.Background())
		return nil
	})
	if err != nil || !released {
		t.Fatal("startup gate did not return safely", err)
	}
	delayed(context.Background())
	if app.store != nil {
		t.Fatal("delayed callback reopened database without a lock")
	}
	if _, err := os.Stat(filepath.Join(directory, "manga.sqlite")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("delayed callback touched database: %v", err)
	}
}

func TestShutdownWaitsForStartedInitialization(t *testing.T) {
	app := NewApp()
	directory := t.TempDir()
	entered, release := make(chan struct{}), make(chan struct{})
	app.dataDirectory = func() (string, error) {
		close(entered)
		<-release
		return directory, nil
	}
	startupDone, shutdownDone := make(chan struct{}), make(chan struct{})
	go func() { app.startup(context.Background()); close(startupDone) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("startup did not enter initialization")
	}
	go func() { app.shutdown(context.Background()); close(shutdownDone) }()
	select {
	case <-shutdownDone:
		t.Fatal("shutdown did not wait for initialization")
	default:
	}
	close(release)
	for _, done := range []chan struct{}{startupDone, shutdownDone} {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("lifecycle did not finish")
		}
	}
	if app.store != nil {
		t.Fatal("shutdown left database open")
	}
	// If stopped is not checked, another callback calls the directory function
	// again (and attempts to close entered twice), in addition to reopening DB.
	app.startup(context.Background())
	if app.store != nil {
		t.Fatal("stopped App reopened")
	}
}
