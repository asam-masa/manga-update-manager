package storage

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/settings"
)

func dateEdit(work manga.Work, date manga.ScheduledDate) manga.EditWorkInput {
	input := textOf(work)
	input.ScheduledDate = &date
	return input
}

func TestScheduledDateLifecycleAndOtherOperations(t *testing.T) {
	ctx := context.Background()
	s, path := managedRecordStore(t)
	w := coverReferenceWork(t, s)
	date := manga.ScheduledDate("2028-02-29")
	got, err := s.EditWork(ctx, w.ID, dateEdit(w, date), w.UpdatedAt.Add(time.Hour))
	if err != nil || got.ScheduledDate != date || got.UpdatedAt.Equal(w.UpdatedAt) {
		t.Fatal(got, err)
	}
	w = got
	for _, input := range []manga.EditWorkInput{textOf(w), dateEdit(w, date)} {
		got, err := s.EditWork(ctx, w.ID, input, w.UpdatedAt.Add(time.Hour))
		if err != nil || !reflect.DeepEqual(got, w) {
			t.Fatal("no-op changed work", got, err)
		}
	}
	input := textOf(w)
	input.Title = "Text only"
	w, err = s.EditWork(ctx, w.ID, input, w.UpdatedAt.Add(time.Hour))
	if err != nil || w.ScheduledDate != date {
		t.Fatal(w, err)
	}
	if err := s.RecordLastAccess(ctx, w.ID, w.UpdatedAt.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	readyCover(t, s, testCoverID)
	w, _, err = s.SetWorkCover(ctx, w.ID, testCoverID, w.UpdatedAt.Add(time.Hour))
	if err != nil || w.ScheduledDate != date {
		t.Fatal(w, err)
	}
	w, _, err = s.RemoveWorkCover(ctx, w.ID, w.UpdatedAt.Add(time.Hour))
	if err != nil || w.ScheduledDate != date {
		t.Fatal(w, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err = s.WorkByID(ctx, w.ID)
	if err != nil || !reflect.DeepEqual(got, w) {
		t.Fatal(got, err)
	}
	values, err := s.ListWorks(ctx)
	if err != nil || len(values) != 1 || !reflect.DeepEqual(values[0], w) {
		t.Fatal(values, err)
	}
	w, err = s.EditWork(ctx, w.ID, dateEdit(w, ""), w.UpdatedAt.Add(time.Hour))
	if err != nil || w.ScheduledDate != "" {
		t.Fatal(w, err)
	}
	var raw sql.NullString
	if err := s.db.QueryRow("SELECT scheduled_date FROM works WHERE id=?", w.ID).Scan(&raw); err != nil || raw.Valid {
		t.Fatal(raw, err)
	}
	if _, err := s.DeleteWork(ctx, w.ID, deleteInput(got)); !errors.Is(err, ErrWorkDeleteChanged) {
		t.Fatal("stale date confirmation accepted", err)
	}
	if _, err := s.DeleteWork(ctx, w.ID, deleteInput(w)); err != nil {
		t.Fatal(err)
	}
}

func TestScheduledDateCreateDeleteAndFailure(t *testing.T) {
	ctx := context.Background()
	s, _ := managedRecordStore(t)
	w, err := manga.NewWork(manga.NewWorkInput{URL: "https://example.com/date", Title: "Date"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	w.ScheduledDate = "2026-10-30"
	w, err = s.CreateWork(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := s.db.QueryRow("SELECT scheduled_date FROM works WHERE id=?", w.ID).Scan(&raw); err != nil || raw != "2026-10-30" {
		t.Fatal(raw, err)
	}
	invalid := dateEdit(w, "2026-02-30")
	if _, err := s.EditWork(ctx, w.ID, invalid, w.UpdatedAt); !errors.Is(err, manga.ErrScheduledDate) {
		t.Fatal(err)
	}
	bad := w
	bad.ID = 0
	bad.URL = "https://example.com/invalid"
	bad.ScheduledDate = "2026-02-30"
	if _, err := s.CreateWork(ctx, bad); !errors.Is(err, manga.ErrScheduledDate) {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("CREATE TRIGGER reject_date BEFORE UPDATE ON works BEGIN SELECT RAISE(ABORT,'synthetic rejection'); END"); err != nil {
		t.Fatal(err)
	}
	input := dateEdit(w, "2026-11-01")
	input.Title = "Both must rollback"
	if _, err := s.EditWork(ctx, w.ID, input, w.UpdatedAt.Add(time.Hour)); err == nil {
		t.Fatal("write accepted")
	}
	got, err := s.WorkByID(ctx, w.ID)
	if err != nil || !reflect.DeepEqual(got, w) {
		t.Fatal(got, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.EditWork(cancelled, w.ID, input, w.UpdatedAt); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	expected := deleteInput(w)
	expected.ScheduledDate = ""
	if _, err := s.DeleteWork(ctx, w.ID, expected); !errors.Is(err, ErrWorkDeleteChanged) {
		t.Fatal(err)
	}
	if _, err := s.DeleteWork(ctx, w.ID, deleteInput(w)); err != nil {
		t.Fatal(err)
	}
}

func TestScheduledDateConstraintsAndCorruptRead(t *testing.T) {
	s, _ := managedRecordStore(t)
	w := coverReferenceWork(t, s)
	for _, raw := range []string{"", "0000-01-01", "2026-1-01", "2026-01-01T00:00:00Z"} {
		if _, err := s.db.Exec("UPDATE works SET scheduled_date=? WHERE id=?", raw, w.ID); err == nil {
			t.Fatal("missing shape constraint", raw)
		}
	}
	// Full calendar validity belongs to the domain, not SQLite date coercion.
	if _, err := s.db.Exec("UPDATE works SET scheduled_date='2026-02-30' WHERE id=?", w.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WorkByID(context.Background(), w.ID); !errors.Is(err, manga.ErrScheduledDate) {
		t.Fatal(err)
	}
	if _, err := s.ListWorks(context.Background()); !errors.Is(err, manga.ErrScheduledDate) {
		t.Fatal(err)
	}
	var raw string
	if err := s.db.QueryRow("SELECT scheduled_date FROM works WHERE id=?", w.ID).Scan(&raw); err != nil || raw != "2026-02-30" {
		t.Fatal(raw, err)
	}
}

func TestScheduledDateMigrationPreservesSixSchema(t *testing.T) {
	ctx := context.Background()
	old, path := previousCoverSchema(t)
	for _, name := range []string{"004_managed_covers.sql", "005_work_list_mode.sql", "006_work_sort.sql"} {
		if err := applyMigration(ctx, old.db, "migrations/"+name); err != nil {
			t.Fatal(err)
		}
	}
	w := legacyCoverWork(t, old)
	readyCover(t, old, testCoverID)
	cover, err := old.ManagedCoverByID(ctx, testCoverID)
	if err != nil {
		t.Fatal(err)
	}
	if err := old.SetRegistrationFormVisible(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := old.SetWorkListMode(ctx, settings.Rows); err != nil {
		t.Fatal(err)
	}
	if err := old.SetWorkSort(ctx, settings.TitleDesc); err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		s, err := Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.WorkByID(ctx, w.ID)
		if err != nil || !reflect.DeepEqual(got, w) {
			t.Fatal(got, err)
		}
		var raw sql.NullString
		if err := s.db.QueryRow("SELECT scheduled_date FROM works WHERE id=?", w.ID).Scan(&raw); err != nil || raw.Valid {
			t.Fatal(raw, err)
		}
		if got, err := s.ManagedCoverByID(ctx, testCoverID); err != nil || !reflect.DeepEqual(got, cover) {
			t.Fatal(got, err)
		}
		if got, err := s.DisplaySettings(ctx); err != nil || got.RegistrationFormVisible {
			t.Fatal(got, err)
		}
		if got, err := s.WorkListMode(ctx); err != nil || got != settings.Rows {
			t.Fatal(got, err)
		}
		if got, err := s.WorkSort(ctx); err != nil || got != settings.TitleDesc {
			t.Fatal(got, err)
		}
		var count int
		if err := s.db.QueryRow("SELECT count(*) FROM schema_migrations WHERE version='007_scheduled_date.sql'").Scan(&count); err != nil || count != 1 {
			t.Fatal(count, err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
