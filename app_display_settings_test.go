package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestDisplaySettingsAPIReopensAndKeepsWorks(t *testing.T) {
	directory := t.TempDir()
	app := testApp(t, directory)
	value, err := app.LoadDisplaySettings()
	if err != nil || !value.RegistrationFormVisible {
		t.Fatalf("default = %#v, %v", value, err)
	}
	work, err := app.CreateWork(CreateWorkInput{URL: "https://example.com/1", Title: "作品"})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.SetRegistrationFormVisible(false); err != nil {
		t.Fatal(err)
	}
	app.shutdown(context.Background())
	if _, err := app.LoadDisplaySettings(); errorCode(t, err) != "settings_read_failed" {
		t.Fatal(err)
	}
	if err := app.SetRegistrationFormVisible(true); errorCode(t, err) != "settings_save_failed" {
		t.Fatal(err)
	}
	reopened := testApp(t, directory)
	value, err = reopened.LoadDisplaySettings()
	if err != nil || value.RegistrationFormVisible {
		t.Fatalf("reopened = %#v, %v", value, err)
	}
	works, err := reopened.ListWorks()
	if err != nil || len(works) != 1 || works[0] != work {
		t.Fatalf("works = %#v, %v", works, err)
	}
}

func TestDisplaySettingsAPIErrorsDoNotExposeDiagnostics(t *testing.T) {
	app := testApp(t, t.TempDir())
	app.store.Close()
	_, readErr := app.LoadDisplaySettings()
	saveErr := app.SetRegistrationFormVisible(false)
	for _, err := range []error{readErr, saveErr} {
		code := errorCode(t, err)
		if code != "settings_read_failed" && code != "settings_save_failed" {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(formatAPIError(err))
		if strings.Contains(string(encoded), "database is closed") {
			t.Fatalf("unsafe error: %s", encoded)
		}
	}
}
