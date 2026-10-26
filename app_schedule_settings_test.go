package main

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

func TestFutureSchedulesAPILifecycle(t *testing.T) {
	dir := t.TempDir()
	a := testApp(t, dir)
	db, err := sql.Open("sqlite", filepath.Join(dir, "manga.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if got, err := a.LoadShowFutureSchedules(); err != nil || !got {
		t.Fatal(got, err)
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM display_settings").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	w, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/future", Title: "予定確認"})
	if err != nil {
		t.Fatal(err)
	}
	date := "2028-02-29"
	edit, err := a.EditWork(w.ID, EditWorkInput{URL: w.URL, Title: w.Title, ScheduledDate: &date})
	if err != nil {
		t.Fatal(err)
	}
	w = edit.Work
	if err := a.SetRegistrationFormVisible(false); err != nil {
		t.Fatal(err)
	}
	if err := a.SetWorkListMode("rows"); err != nil {
		t.Fatal(err)
	}
	if err := a.SetWorkSort("scheduled_date"); err != nil {
		t.Fatal(err)
	}
	for _, visible := range []bool{true, false} {
		if err := a.SetShowFutureSchedules(visible); err != nil {
			t.Fatal(err)
		}
		if got, err := a.LoadShowFutureSchedules(); err != nil || got != visible {
			t.Fatal(got, err)
		}
	}
	a.shutdown(context.Background())
	if _, err := a.LoadShowFutureSchedules(); errorCode(t, err) != "settings_read_failed" {
		t.Fatal(err)
	}
	if err := a.SetShowFutureSchedules(true); errorCode(t, err) != "settings_save_failed" {
		t.Fatal(err)
	}
	reopened := testApp(t, dir)
	if got, err := reopened.LoadShowFutureSchedules(); err != nil || got {
		t.Fatal(got, err)
	}
	if form, err := reopened.LoadDisplaySettings(); err != nil || form.RegistrationFormVisible {
		t.Fatal(form, err)
	}
	if mode, err := reopened.LoadWorkListMode(); err != nil || mode != "rows" {
		t.Fatal(mode, err)
	}
	if order, err := reopened.LoadWorkSort(); err != nil || order != "scheduled_date" {
		t.Fatal(order, err)
	}
	if works, err := reopened.ListWorks(); err != nil || len(works) != 1 || !reflect.DeepEqual(works[0], w) {
		t.Fatal(works, err)
	}
}

func TestFutureSchedulesAPIFailureSafety(t *testing.T) {
	for _, cause := range []error{errors.New("private diagnostic"), context.Canceled, errors.Join(storage.ErrDBStopped, errors.New("private diagnostic"))} {
		a := NewApp()
		a.ctx = context.Background()
		a.display = application.NewDisplaySettings(failedDisplayRepository{cause})
		got, readErr := a.LoadShowFutureSchedules()
		saveErr := a.SetShowFutureSchedules(false)
		if got {
			t.Fatal("returned fallback as saved value on error")
		}
		for _, tc := range []struct {
			err  error
			code string
		}{{readErr, "settings_read_failed"}, {saveErr, "settings_save_failed"}} {
			want := tc.code
			if errors.Is(cause, storage.ErrDBStopped) {
				want = "database_stopped"
			}
			if errorCode(t, tc.err) != want || strings.Contains(tc.err.Error(), "private") || strings.Contains(tc.err.Error(), "canceled") {
				t.Fatal(tc.err)
			}
		}
		if errors.Is(cause, storage.ErrDBStopped) {
			if _, err := a.coverOperations.BeginRead(); !errors.Is(err, application.ErrCoverStopped) {
				t.Fatal(err)
			}
		} else {
			op, err := a.coverOperations.BeginRead()
			if err != nil {
				t.Fatal(err)
			}
			_ = op.End()
		}
	}
	a := NewApp()
	if _, err := a.LoadShowFutureSchedules(); errorCode(t, err) != "settings_read_failed" {
		t.Fatal(err)
	}
	if err := a.SetShowFutureSchedules(true); errorCode(t, err) != "settings_save_failed" {
		t.Fatal(err)
	}
	a = testApp(t, t.TempDir())
	if err := a.store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.LoadShowFutureSchedules(); errorCode(t, err) != "database_stopped" {
		t.Fatal(err)
	}
	if err := a.SetShowFutureSchedules(false); errorCode(t, err) != "database_stopped" {
		t.Fatal(err)
	}
}

func TestFutureSchedulesAPIUnknownAndWriteFailure(t *testing.T) {
	dir := t.TempDir()
	a := testApp(t, dir)
	if err := a.SetShowFutureSchedules(false); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "manga.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("PRAGMA ignore_check_constraints=ON; UPDATE display_settings SET show_future_schedules=2; PRAGMA ignore_check_constraints=OFF"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.LoadShowFutureSchedules(); errorCode(t, err) != "settings_read_failed" || !strings.Contains(err.Error(), "未来の予定を表示して続行") {
		t.Fatal(err)
	}
	var raw int
	if err := db.QueryRow("SELECT show_future_schedules FROM display_settings").Scan(&raw); err != nil || raw != 2 {
		t.Fatal(raw, err)
	}
	if err := a.SetShowFutureSchedules(false); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TRIGGER reject_future BEFORE UPDATE ON display_settings BEGIN SELECT RAISE(ABORT, 'private diagnostic'); END"); err != nil {
		t.Fatal(err)
	}
	if err := a.SetShowFutureSchedules(true); errorCode(t, err) != "settings_save_failed" || strings.Contains(err.Error(), "private") {
		t.Fatal(err)
	}
	if got, err := a.LoadShowFutureSchedules(); err != nil || got {
		t.Fatal(got, err)
	}
}
