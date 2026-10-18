package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/asam-masa/manga-update-manager/internal/application"
	"strings"
	"testing"
	"time"
)

func TestAccessAPI(t *testing.T) {
	directory := t.TempDir()
	app := NewApp()
	app.dataDirectory = func() (string, error) { return directory, nil }
	app.openURL = func(string) error { return nil }
	app.startup(context.Background())
	defer app.shutdown(context.Background())
	work, err := app.CreateWork(CreateWorkInput{URL: "https://example.com", Title: "作品"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(work)
	if !strings.Contains(string(raw), `"lastAccessedAt":null`) {
		t.Fatal(string(raw))
	}
	at := time.Date(2026, 10, 9, 3, 0, 0, 123, time.UTC)
	app.opener = application.NewWorkOpener(app.store, app.openURL, func() time.Time { return at })
	opened, err := app.OpenWork(work.ID)
	if err != nil || opened.LastAccessedAt == nil || *opened.LastAccessedAt != "2026-10-09T12:00:00.000000123+09:00" {
		t.Fatal(opened, err)
	}
	app.opener = application.NewWorkOpener(app.store, func(string) error { return errors.New("private diagnostic") }, time.Now)
	if _, err = app.OpenWork(work.ID); errorCode(t, err) != "browser_open_failed" {
		t.Fatal(err)
	}
	list, err := app.ListWorks()
	if err != nil || *list[0].LastAccessedAt != *opened.LastAccessedAt {
		t.Fatal(list, err)
	}
	app.opener = application.NewWorkOpener(app.store, func(string) error { return app.store.Close() }, time.Now)
	if _, err = app.OpenWork(work.ID); errorCode(t, err) != "access_save_failed" {
		t.Fatal(err)
	}
	if !strings.Contains(err.Error(), "ページを開きました") || !strings.Contains(err.Error(), "再起動して状態を確認") {
		t.Fatal("lost partial success or stop notice", err)
	}
	if _, err := app.OpenWork(work.ID); errorCode(t, err) != "database_stopped" {
		t.Fatal("launch after stop", err)
	}
	app.shutdown(context.Background())
	// A restart creates a fresh App; a stopped App cannot reopen its database.
	app = testApp(t, directory)
	app.openURL = func(string) error { return nil }
	list, err = app.ListWorks()
	if err != nil || *list[0].LastAccessedAt != *opened.LastAccessedAt {
		t.Fatal(list, err)
	}
	if _, err = app.OpenWork(999); errorCode(t, err) != "work_not_found" {
		t.Fatal(err)
	}
	app.shutdown(context.Background())
	if _, err = app.OpenWork(work.ID); errorCode(t, err) != "database_unavailable" {
		t.Fatal(err)
	}
}
