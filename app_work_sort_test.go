package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/settings"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

func TestWorkSortAPILifecycle(t *testing.T) {
	dir := t.TempDir()
	a := testApp(t, dir)
	if got, err := a.LoadWorkSort(); err != nil || got != "registration_asc" {
		t.Fatal(got, err)
	}
	w, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/sort", Title: "並び順確認"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetRegistrationFormVisible(false); err != nil {
		t.Fatal(err)
	}
	if err := a.SetWorkListMode("rows"); err != nil {
		t.Fatal(err)
	}
	for _, order := range []string{"registration_asc", "registration_desc", "title_asc", "title_desc", "last_access_asc", "last_access_desc", "scheduled_date"} {
		if err := a.SetWorkSort(order); err != nil {
			t.Fatal(err)
		}
		if got, err := a.LoadWorkSort(); err != nil || got != order {
			t.Fatal(got, err)
		}
	}
	if err := a.SetWorkSort("TITLE_ASC"); errorCode(t, err) != "invalid_input" {
		t.Fatal(err)
	}
	a.shutdown(context.Background())
	if _, err := a.LoadWorkSort(); errorCode(t, err) != "settings_read_failed" {
		t.Fatal(err)
	}
	if err := a.SetWorkSort("title_asc"); errorCode(t, err) != "settings_save_failed" {
		t.Fatal(err)
	}
	reopened := testApp(t, dir)
	if got, err := reopened.LoadWorkSort(); err != nil || got != "scheduled_date" {
		t.Fatal(got, err)
	}
	if form, err := reopened.LoadDisplaySettings(); err != nil || form.RegistrationFormVisible {
		t.Fatal(form, err)
	}
	if mode, err := reopened.LoadWorkListMode(); err != nil || mode != "rows" {
		t.Fatal(mode, err)
	}
	if works, err := reopened.ListWorks(); err != nil || len(works) != 1 || works[0] != w {
		t.Fatal(works, err)
	}
}

func TestWorkSortAPIFailureSafety(t *testing.T) {
	for _, cause := range []error{errors.New("private diagnostic"), settings.ErrWorkSort, errors.Join(storage.ErrDBStopped, errors.New("private diagnostic"))} {
		a := NewApp()
		a.ctx = context.Background()
		a.display = application.NewDisplaySettings(failedDisplayRepository{cause})
		got, readErr := a.LoadWorkSort()
		saveErr := a.SetWorkSort("title_asc")
		if got != "" {
			t.Fatal("returned usable value on failure", got)
		}
		for _, tc := range []struct {
			err  error
			code string
		}{{readErr, "settings_read_failed"}, {saveErr, "settings_save_failed"}} {
			want := tc.code
			if errors.Is(cause, storage.ErrDBStopped) {
				want = "database_stopped"
			} else if errors.Is(cause, settings.ErrWorkSort) && tc.err == saveErr {
				want = "invalid_input"
			}
			if errorCode(t, tc.err) != want || strings.Contains(tc.err.Error(), "private") {
				t.Fatal(tc.err)
			}
		}
	}
	a := testApp(t, t.TempDir())
	if err := a.store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.LoadWorkSort(); errorCode(t, err) != "database_stopped" {
		t.Fatal(err)
	}
	if err := a.SetWorkSort("title_asc"); errorCode(t, err) != "database_stopped" {
		t.Fatal(err)
	}
}
