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

func TestWorkListModeAPILifecycle(t *testing.T) {
	dir := t.TempDir()
	a := testApp(t, dir)
	if mode, err := a.LoadWorkListMode(); err != nil || mode != "cards" {
		t.Fatal(mode, err)
	}
	if err := a.SetRegistrationFormVisible(false); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"compact", "rows", "cards"} {
		if err := a.SetWorkListMode(mode); err != nil {
			t.Fatal(err)
		}
		if got, err := a.LoadWorkListMode(); err != nil || got != mode {
			t.Fatal(got, err)
		}
		if form, err := a.LoadDisplaySettings(); err != nil || form.RegistrationFormVisible {
			t.Fatal(form, err)
		}
	}
	if err := a.SetWorkListMode("rows"); err != nil {
		t.Fatal(err)
	}
	if err := a.SetWorkListMode("ROWS"); errorCode(t, err) != "invalid_input" {
		t.Fatal(err)
	}
	a.shutdown(context.Background())
	reopened := testApp(t, dir)
	if mode, err := reopened.LoadWorkListMode(); err != nil || mode != "rows" {
		t.Fatal(mode, err)
	}
	if form, err := reopened.LoadDisplaySettings(); err != nil || form.RegistrationFormVisible {
		t.Fatal(form, err)
	}
	if _, err := a.LoadWorkListMode(); errorCode(t, err) != "settings_read_failed" {
		t.Fatal(err)
	}
	if err := a.SetWorkListMode("rows"); errorCode(t, err) != "settings_save_failed" {
		t.Fatal(err)
	}
}

func TestWorkListModeAPIFailureSafety(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		var cause error = errors.New("private diagnostic")
		if stopped {
			cause = errors.Join(cause, storage.ErrDBStopped)
		}
		a := NewApp()
		a.ctx = context.Background()
		a.display = application.NewDisplaySettings(failedDisplayRepository{cause})
		_, readErr := a.LoadWorkListMode()
		saveErr := a.SetWorkListMode("rows")
		for _, tc := range []struct {
			err  error
			code string
		}{{readErr, "settings_read_failed"}, {saveErr, "settings_save_failed"}} {
			want := tc.code
			if stopped {
				want = "database_stopped"
			}
			if errorCode(t, tc.err) != want || strings.Contains(tc.err.Error(), "private") {
				t.Fatal(tc.err)
			}
		}
	}
	// An unknown stored value follows the read-warning contract, not a write retry.
	a := NewApp()
	a.ctx = context.Background()
	a.display = application.NewDisplaySettings(failedDisplayRepository{settings.ErrWorkListMode})
	if _, err := a.LoadWorkListMode(); errorCode(t, err) != "settings_read_failed" || !strings.Contains(err.Error(), "メモなしカード") {
		t.Fatal(err)
	}
}
