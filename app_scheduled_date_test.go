package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/manga"
)

func scheduleEditInput(w WorkDTO, date *string) EditWorkInput {
	return EditWorkInput{URL: w.URL, Title: w.Title, SiteName: w.SiteName, Notes: w.Notes, ScheduledDate: date}
}

func TestScheduledDateAPIWireAndPersistence(t *testing.T) {
	dir := t.TempDir()
	a := testApp(t, dir)
	w, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/schedule-api", Title: "作品"})
	if err != nil || w.ScheduledDate != nil {
		t.Fatal(w, err)
	}
	wire, err := json.Marshal(w)
	if err != nil || !strings.Contains(string(wire), `"scheduledDate":null`) {
		t.Fatal(string(wire), err)
	}
	for _, tc := range []struct{ fragment, want string }{
		{`,"scheduledDate":"2028-02-29"`, "2028-02-29"},
		{"", "2028-02-29"}, {`,"scheduledDate":null`, "2028-02-29"},
		{`,"scheduledDate":""`, ""}, {`,"scheduledDate":"2026-10-30"`, "2026-10-30"},
	} {
		var input EditWorkInput
		if err := json.Unmarshal([]byte(`{"url":"https://example.com/schedule-api","title":"作品"`+tc.fragment+`}`), &input); err != nil {
			t.Fatal(err)
		}
		result, err := a.EditWork(w.ID, input)
		if err != nil || len(result.Warnings) != 0 {
			t.Fatal(result, err)
		}
		w = result.Work
		if tc.want == "" {
			if w.ScheduledDate != nil {
				t.Fatal(w)
			}
		} else if w.ScheduledDate == nil || *w.ScheduledDate != tc.want {
			t.Fatal(w)
		}
	}
	// Browser dispatch is injected: never open a real URL from an API test.
	a.opener = application.NewWorkOpener(a.store, func(string) error { return nil }, time.Now)
	w, err = a.OpenWork(w.ID)
	if err != nil || w.ScheduledDate == nil || *w.ScheduledDate != "2026-10-30" {
		t.Fatal(w, err)
	}
	a.shutdown(context.Background())
	a = testApp(t, dir)
	values, err := a.ListWorks()
	if err != nil || len(values) != 1 || values[0].ScheduledDate == nil || *values[0].ScheduledDate != "2026-10-30" {
		t.Fatal(values, err)
	}
	w = values[0]
	before := w.UpdatedAt
	for _, date := range []string{"2026-02-29", "2026-04-31", "0000-01-01", "2026-1-01", " 2026-10-30", "2026-10-30T00:00:00Z"} {
		result, err := a.EditWork(w.ID, scheduleEditInput(w, &date))
		if errorCode(t, err) != "invalid_input" || result.Work.ID != 0 {
			t.Fatal(result, err)
		}
	}
	values, err = a.ListWorks()
	if err != nil || values[0].UpdatedAt != before || values[0].ScheduledDate == nil || *values[0].ScheduledDate != "2026-10-30" {
		t.Fatal(values, err)
	}
}

func TestScheduledDateAPIDeleteSnapshot(t *testing.T) {
	a := testApp(t, t.TempDir())
	w, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/delete-date", Title: "作品"})
	if err != nil {
		t.Fatal(err)
	}
	old := deleteSnapshot(w)
	date := "2026-10-30"
	result, err := a.EditWork(w.ID, scheduleEditInput(w, &date))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.DeleteWork(w.ID, old); errorCode(t, err) != "work_changed" {
		t.Fatal(err)
	}
	stale := deleteSnapshot(result.Work)
	date = "2026-11-01"
	result, err = a.EditWork(w.ID, scheduleEditInput(result.Work, &date))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.DeleteWork(w.ID, stale); errorCode(t, err) != "work_changed" {
		t.Fatal(err)
	}
	deleted, err := a.DeleteWork(w.ID, deleteSnapshot(result.Work))
	if err != nil || deleted.WorkID != w.ID {
		t.Fatal(deleted, err)
	}
}

func TestScheduledDateStoredErrorIsNotInputError(t *testing.T) {
	a := testApp(t, t.TempDir())
	a.editor = application.NewWorkEditor(appEditRepositoryFunc(func(context.Context, int64, manga.EditWorkInput, time.Time) (manga.Work, error) {
		return manga.Work{}, manga.ErrScheduledDate
	}), time.Now)
	date := "2026-10-30"
	_, err := a.EditWork(1, EditWorkInput{URL: "https://example.com", Title: "作品", ScheduledDate: &date})
	if errorCode(t, err) != "storage_error" {
		t.Fatal(err)
	}
}
