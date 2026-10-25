package storage

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/settings"
)

func TestWorkSortPersistenceAndIsolation(t *testing.T) {
	ctx := context.Background()
	s, path := managedRecordStore(t)
	w := coverReferenceWork(t, s)
	if got, err := s.WorkSort(ctx); err != nil || got != settings.RegistrationAsc {
		t.Fatal(got, err)
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM display_settings").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	// Inserting sort first must use both other preferences' defaults.
	if err := s.SetWorkSort(ctx, settings.TitleDesc); err != nil {
		t.Fatal(err)
	}
	if form, err := s.DisplaySettings(ctx); err != nil || !form.RegistrationFormVisible {
		t.Fatal(form, err)
	}
	if mode, err := s.WorkListMode(ctx); err != nil || mode != settings.Cards {
		t.Fatal(mode, err)
	}
	for _, want := range []settings.WorkSort{settings.RegistrationAsc, settings.RegistrationDesc, settings.TitleAsc, settings.TitleDesc, settings.LastAccessAsc, settings.LastAccessDesc} {
		if err := s.SetRegistrationFormVisible(ctx, false); err != nil {
			t.Fatal(err)
		}
		if err := s.SetWorkListMode(ctx, settings.Rows); err != nil {
			t.Fatal(err)
		}
		if err := s.SetWorkSort(ctx, want); err != nil {
			t.Fatal(err)
		}
		if form, err := s.DisplaySettings(ctx); err != nil || form.RegistrationFormVisible {
			t.Fatal(form, err)
		}
		if mode, err := s.WorkListMode(ctx); err != nil || mode != settings.Rows {
			t.Fatal(mode, err)
		}
		if err := s.SetRegistrationFormVisible(ctx, true); err != nil {
			t.Fatal(err)
		}
		if err := s.SetWorkListMode(ctx, settings.Compact); err != nil {
			t.Fatal(err)
		}
		if got, err := s.WorkSort(ctx); err != nil || got != want {
			t.Fatal(got, err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got, err := reopened.WorkSort(ctx); err != nil || got != settings.LastAccessDesc {
		t.Fatal(got, err)
	}
	if got, err := reopened.WorkByID(ctx, w.ID); err != nil || !reflect.DeepEqual(got, w) {
		t.Fatal(got, err)
	}
}

func TestWorkSortInvalidFailureAndStop(t *testing.T) {
	ctx := context.Background()
	s, _ := managedRecordStore(t)
	if err := s.SetWorkSort(ctx, settings.TitleAsc); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"", "future", "TITLE_ASC", " title_asc", "title_asc ", "title"} {
		if err := s.SetWorkSort(ctx, settings.WorkSort(raw)); !errors.Is(err, settings.ErrWorkSort) {
			t.Fatal(raw, err)
		}
		if _, err := s.db.Exec("UPDATE display_settings SET work_sort=?", raw); err == nil {
			t.Fatal("missing CHECK", raw)
		}
	}
	if _, err := s.db.Exec("PRAGMA ignore_check_constraints=ON; UPDATE display_settings SET work_sort='future'; PRAGMA ignore_check_constraints=OFF"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.WorkSort(ctx); got != settings.RegistrationAsc || !errors.Is(err, settings.ErrWorkSort) {
		t.Fatal(got, err)
	}
	var raw string
	if err := s.db.QueryRow("SELECT work_sort FROM display_settings").Scan(&raw); err != nil || raw != "future" {
		t.Fatal(raw, err)
	}
	if form, err := s.DisplaySettings(ctx); err != nil || !form.RegistrationFormVisible {
		t.Fatal(form, err)
	}
	if mode, err := s.WorkListMode(ctx); err != nil || mode != settings.Cards {
		t.Fatal(mode, err)
	}
	if err := s.SetWorkSort(ctx, settings.TitleAsc); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("CREATE TRIGGER reject_sort BEFORE UPDATE ON display_settings BEGIN SELECT RAISE(ABORT,'private diagnostic'); END"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetWorkSort(ctx, settings.LastAccessDesc); err == nil {
		t.Fatal("write accepted")
	}
	if got, err := s.WorkSort(ctx); err != nil || got != settings.TitleAsc {
		t.Fatal(got, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.WorkSort(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := s.SetWorkSort(cancelled, settings.TitleDesc); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("ALTER TABLE display_settings RENAME COLUMN work_sort TO broken_sort"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WorkSort(ctx); err == nil {
		t.Fatal("unreadable accepted")
	}
	if err := s.SetWorkSort(ctx, settings.TitleDesc); err == nil {
		t.Fatal("missing column accepted")
	}
	if mode, err := s.WorkListMode(ctx); err != nil || mode != settings.Cards {
		t.Fatal(mode, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WorkSort(ctx); !errors.Is(err, ErrDBStopped) {
		t.Fatal(err)
	}
	if err := s.SetWorkSort(ctx, settings.TitleDesc); !errors.Is(err, ErrDBStopped) {
		t.Fatal(err)
	}
}

func TestWorkSortMigrationFromFivePreservesData(t *testing.T) {
	ctx := context.Background()
	old, path := previousCoverSchema(t)
	for _, name := range []string{"004_managed_covers.sql", "005_work_list_mode.sql"} {
		if err := applyMigration(ctx, old.db, "migrations/"+name); err != nil {
			t.Fatal(err)
		}
	}
	w := coverReferenceWork(t, old)
	readyCover(t, old, testCoverID)
	before, err := old.ManagedCoverByID(ctx, testCoverID)
	if err != nil {
		t.Fatal(err)
	}
	if err := old.SetRegistrationFormVisible(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := old.SetWorkListMode(ctx, settings.Rows); err != nil {
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
		if got, err := s.WorkSort(ctx); err != nil || got != settings.RegistrationAsc {
			t.Fatal(got, err)
		}
		if form, err := s.DisplaySettings(ctx); err != nil || form.RegistrationFormVisible {
			t.Fatal(form, err)
		}
		if mode, err := s.WorkListMode(ctx); err != nil || mode != settings.Rows {
			t.Fatal(mode, err)
		}
		if got, err := s.WorkByID(ctx, w.ID); err != nil || !reflect.DeepEqual(got, w) {
			t.Fatal(got, err)
		}
		if got, err := s.ManagedCoverByID(ctx, testCoverID); err != nil || !reflect.DeepEqual(got, before) {
			t.Fatal(got, err)
		}
		var count int
		if err := s.db.QueryRow("SELECT count(*) FROM schema_migrations WHERE version='006_work_sort.sql'").Scan(&count); err != nil || count != 1 {
			t.Fatal(count, err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
