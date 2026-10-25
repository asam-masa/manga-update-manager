package storage

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/settings"
)

func TestWorkListModePersistenceAndFieldIsolation(t *testing.T) {
	ctx := context.Background()
	s, path := managedRecordStore(t)
	work := coverReferenceWork(t, s)
	if mode, err := s.WorkListMode(ctx); err != nil || mode != settings.Cards {
		t.Fatal(mode, err)
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM display_settings").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	// Setting a mode first inserts the form's ordinary default, not a zero value.
	if err := s.SetWorkListMode(ctx, settings.Rows); err != nil {
		t.Fatal(err)
	}
	if form, err := s.DisplaySettings(ctx); err != nil || !form.RegistrationFormVisible {
		t.Fatal(form, err)
	}
	if err := s.SetRegistrationFormVisible(ctx, false); err != nil {
		t.Fatal(err)
	}
	for _, want := range []settings.WorkListMode{settings.Compact, settings.Rows, settings.Cards} {
		if err := s.SetWorkListMode(ctx, want); err != nil {
			t.Fatal(err)
		}
		if form, err := s.DisplaySettings(ctx); err != nil || form.RegistrationFormVisible {
			t.Fatal(form, err)
		}
		if err := s.SetRegistrationFormVisible(ctx, true); err != nil {
			t.Fatal(err)
		}
		if got, err := s.WorkListMode(ctx); err != nil || got != want {
			t.Fatal(got, err)
		}
		if err := s.SetRegistrationFormVisible(ctx, false); err != nil {
			t.Fatal(err)
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
	if mode, err := reopened.WorkListMode(ctx); err != nil || mode != settings.Cards {
		t.Fatal(mode, err)
	}
	if form, err := reopened.DisplaySettings(ctx); err != nil || form.RegistrationFormVisible {
		t.Fatal(form, err)
	}
	if got, err := reopened.WorkByID(ctx, work.ID); err != nil || !reflect.DeepEqual(got, work) {
		t.Fatal(got, err)
	}
}

func TestWorkListModeInvalidAndFailureDoNotRepair(t *testing.T) {
	ctx := context.Background()
	s, _ := managedRecordStore(t)
	work := coverReferenceWork(t, s)
	if err := s.SetRegistrationFormVisible(ctx, false); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"", "invalid", "ROWS", " rows "} {
		if err := s.SetWorkListMode(ctx, settings.WorkListMode(raw)); !errors.Is(err, settings.ErrWorkListMode) {
			t.Fatal(err)
		}
		if _, err := s.db.Exec("UPDATE display_settings SET work_list_mode=?", raw); err == nil {
			t.Fatal("missing CHECK")
		}
	}
	if _, err := s.db.Exec("PRAGMA ignore_check_constraints=ON; UPDATE display_settings SET work_list_mode='future'; PRAGMA ignore_check_constraints=OFF"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.WorkListMode(ctx); got != settings.Cards || !errors.Is(err, settings.ErrWorkListMode) {
		t.Fatal(got, err)
	}
	var raw string
	if err := s.db.QueryRow("SELECT work_list_mode FROM display_settings").Scan(&raw); err != nil || raw != "future" {
		t.Fatal(raw, err)
	}
	if form, err := s.DisplaySettings(ctx); err != nil || form.RegistrationFormVisible {
		t.Fatal(form, err)
	}
	if err := s.SetWorkListMode(ctx, settings.Rows); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("CREATE TRIGGER reject_mode BEFORE UPDATE ON display_settings BEGIN SELECT RAISE(ABORT,'private diagnostic'); END"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetWorkListMode(ctx, settings.Compact); err == nil {
		t.Fatal("write accepted")
	}
	if got, err := s.WorkListMode(ctx); err != nil || got != settings.Rows {
		t.Fatal(got, err)
	}
	if got, err := s.WorkByID(ctx, work.ID); err != nil || !reflect.DeepEqual(got, work) {
		t.Fatal(got, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.WorkListMode(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := s.SetWorkListMode(cancelled, settings.Cards); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WorkListMode(ctx); !errors.Is(err, ErrDBStopped) {
		t.Fatal(err)
	}
	if err := s.SetWorkListMode(ctx, settings.Cards); !errors.Is(err, ErrDBStopped) {
		t.Fatal(err)
	}
}

func TestWorkListModeMigrationPreservesOldSettingsAndWorks(t *testing.T) {
	ctx := context.Background()
	old, path := previousCoverSchema(t)
	if err := applyMigration(ctx, old.db, "migrations/004_managed_covers.sql"); err != nil {
		t.Fatal(err)
	}
	work := legacyCoverWork(t, old)
	if err := old.SetRegistrationFormVisible(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if mode, err := s.WorkListMode(ctx); err != nil || mode != settings.Cards {
		t.Fatal(mode, err)
	}
	if form, err := s.DisplaySettings(ctx); err != nil || form.RegistrationFormVisible {
		t.Fatal(form, err)
	}
	if got, err := s.WorkByID(ctx, work.ID); err != nil || !reflect.DeepEqual(got, work) {
		t.Fatal(got, err)
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM schema_migrations WHERE version='005_work_list_mode.sql'").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

func TestWorkListModeReadFailureDoesNotStopOtherSettings(t *testing.T) {
	ctx := context.Background()
	s, _ := managedRecordStore(t)
	if err := s.SetRegistrationFormVisible(ctx, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("ALTER TABLE display_settings RENAME COLUMN work_list_mode TO broken_mode"); err != nil {
		t.Fatal(err)
	}
	if mode, err := s.WorkListMode(ctx); err == nil || mode != settings.Cards {
		t.Fatal(mode, err)
	}
	if form, err := s.DisplaySettings(ctx); err != nil || form.RegistrationFormVisible {
		t.Fatal(form, err)
	}
	if err := s.SetWorkListMode(ctx, settings.Rows); err == nil {
		t.Fatal("write must fail")
	}
}
