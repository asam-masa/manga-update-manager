package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/settings"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

func TestStoppedAPIErrorPriorityAndSafety(t *testing.T) {
	for _, partial := range []bool{false, true} {
		cause := errors.Join(storage.ErrDBStopped, manga.ErrWorkNotFound, errors.New("private diagnostic"))
		want := "database_stopped"
		if partial {
			cause = errors.Join(cause, application.ErrAccessSave)
			want = "access_save_failed"
		}
		got := workAPIError(cause)
		if errorCode(t, got) != want {
			t.Fatal(got)
		}
		if !strings.Contains(got.Error(), "再起動して状態を確認") || strings.Contains(got.Error(), "ページを開きました") != partial {
			t.Fatal(got)
		}
		encoded, err := json.Marshal(formatAPIError(got))
		if err != nil || strings.Contains(string(encoded), "private") {
			t.Fatalf("unsafe error: %s %v", encoded, err)
		}
	}
}

type failedDisplayRepository struct{ failure error }

func TestSaveCauseDoesNotHidePartialSuccess(t *testing.T) {
	for _, cause := range []error{manga.ErrWorkNotFound, manga.ErrDuplicateURL, errors.New("private diagnostic")} {
		got := workAPIError(errors.Join(application.ErrAccessSave, cause))
		if errorCode(t, got) != "access_save_failed" || !strings.Contains(got.Error(), "ページを開きました") {
			t.Fatal(got)
		}
	}
}

func (r failedDisplayRepository) DisplaySettings(context.Context) (settings.Display, error) {
	return settings.DefaultDisplay(), r.failure
}
func (r failedDisplayRepository) SetRegistrationFormVisible(context.Context, bool) error {
	return r.failure
}

func TestDisplayFailureCodesDistinguishStop(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		failure := errors.New("private diagnostic")
		var cause error = failure
		if stopped {
			cause = errors.Join(storage.ErrDBStopped, failure)
		}
		app := NewApp()
		app.ctx = context.Background()
		app.display = application.NewDisplaySettings(failedDisplayRepository{cause})
		_, readErr := app.LoadDisplaySettings()
		saveErr := app.SetRegistrationFormVisible(false)
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
}
