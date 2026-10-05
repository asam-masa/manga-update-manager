package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
)

func TestAppStatus(t *testing.T) {
	app := NewApp()
	if got := app.Status(); got != "ready" {
		t.Fatalf("Status() = %q, want %q", got, "ready")
	}
}

func testApp(t *testing.T, directory string) *App {
	t.Helper()
	app := NewApp()
	app.dataDirectory = func() (string, error) { return directory, nil }
	app.startup(context.Background())
	t.Cleanup(func() { app.shutdown(context.Background()) })
	return app
}

func errorCode(t *testing.T, err error) string {
	t.Helper()
	var apiError *APIError
	if !errors.As(err, &apiError) {
		t.Fatalf("expected APIError: %v", err)
	}
	return apiError.Code
}

func TestWorkAPILifecycle(t *testing.T) {
	directory := t.TempDir()
	app := testApp(t, directory)
	list, err := app.ListWorks()
	encoded, _ := json.Marshal(list)
	if err != nil || string(encoded) != "[]" {
		t.Fatalf("empty list = %s, %v", encoded, err)
	}
	for _, url := range []string{"https://example.com/z", "https://example.com/a"} {
		work, err := app.CreateWork(CreateWorkInput{URL: url, Title: " 作品 ", Notes: " メモ "})
		if err != nil || work.Title != "作品" || work.Notes != "メモ" || !strings.HasSuffix(work.CreatedAt, "+09:00") {
			t.Fatalf("create = %#v, %v", work, err)
		}
		if _, err := time.Parse(time.RFC3339Nano, work.CreatedAt); err != nil {
			t.Fatal(err)
		}
	}
	list, err = app.ListWorks()
	if err != nil || len(list) != 2 || list[0].ID >= list[1].ID || list[0].URL != "https://example.com/z" {
		t.Fatalf("list = %#v, %v", list, err)
	}
	_, err = app.CreateWork(CreateWorkInput{URL: list[0].URL, Title: "重複"})
	if errorCode(t, err) != "duplicate_url" {
		t.Fatal(err)
	}
	_, err = app.CreateWork(CreateWorkInput{})
	if errorCode(t, err) != "invalid_input" {
		t.Fatal(err)
	}
	app.shutdown(context.Background())
	if _, err := app.ListWorks(); errorCode(t, err) != "database_unavailable" {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, "manga.sqlite")); err != nil {
		t.Fatal(err)
	}
	reopened := testApp(t, directory)
	list, err = reopened.ListWorks()
	if err != nil || len(list) != 2 {
		t.Fatalf("reopen = %#v, %v", list, err)
	}
}

func TestInitializationFailuresAreSafeAPIErrors(t *testing.T) {
	for _, fail := range []string{"directory", "database"} {
		t.Run(fail, func(t *testing.T) {
			app := NewApp()
			directory := t.TempDir()
			if fail == "database" {
				if err := os.Mkdir(filepath.Join(directory, "manga.sqlite"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			app.dataDirectory = func() (string, error) {
				if fail == "directory" {
					return "", errors.New("private path diagnostic")
				}
				return directory, nil
			}
			app.startup(context.Background())
			defer app.shutdown(context.Background())
			_, err := app.ListWorks()
			if errorCode(t, err) != "database_unavailable" {
				t.Fatal(err)
			}
			_, err = app.CreateWork(CreateWorkInput{})
			if errorCode(t, err) != "database_unavailable" {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(formatAPIError(err))
			if !strings.Contains(string(encoded), "database_unavailable") || strings.Contains(string(encoded), directory) || strings.Contains(string(encoded), "private") {
				t.Fatalf("unsafe formatted error: %s", encoded)
			}
		})
	}
	if got := formatAPIError(errors.New("private diagnostic")).(*APIError); got.Code != "internal_error" || strings.Contains(got.Message, "private") {
		t.Fatal(got)
	}
}

func TestWorkDTOKeepsInstantAndNanoseconds(t *testing.T) {
	now := time.Date(2026, 10, 5, 18, 30, 0, 123, time.UTC)
	dto := workDTO(manga.Work{CreatedAt: now, UpdatedAt: now})
	if dto.CreatedAt != "2026-10-06T03:30:00.000000123+09:00" || dto.UpdatedAt != dto.CreatedAt {
		t.Fatalf("Japan DTO = %#v", dto)
	}
}

func TestStorageFailureBecomesSafeError(t *testing.T) {
	app := testApp(t, t.TempDir())
	if err := app.store.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := app.ListWorks()
	if errorCode(t, err) != "storage_error" {
		t.Fatal(err)
	}
	_, err = app.CreateWork(CreateWorkInput{URL: "https://example.com/1", Title: "作品"})
	if errorCode(t, err) != "storage_error" {
		t.Fatal(err)
	}
}
