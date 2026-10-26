package storage

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/settings"
)

func TestFutureSchedulesPersistenceAndIsolation(t *testing.T) {
	ctx := context.Background()
	s, path := managedRecordStore(t)
	if got, err := s.ShowFutureSchedules(ctx); err != nil || !got {
		t.Fatal(got, err)
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM display_settings").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	// Saving this setting first must retain the normal defaults for other fields.
	if err := s.SetShowFutureSchedules(ctx, false); err != nil {
		t.Fatal(err)
	}
	if form, err := s.DisplaySettings(ctx); err != nil || !form.RegistrationFormVisible {
		t.Fatal(form, err)
	}
	if mode, err := s.WorkListMode(ctx); err != nil || mode != settings.Cards {
		t.Fatal(mode, err)
	}
	if order, err := s.WorkSort(ctx); err != nil || order != settings.RegistrationAsc {
		t.Fatal(order, err)
	}
	w := coverReferenceWork(t, s)
	w, err := s.EditWork(ctx, w.ID, dateEdit(w, manga.ScheduledDate("2028-02-29")), w.UpdatedAt.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for _, visible := range []bool{true, false} {
		if err := s.SetRegistrationFormVisible(ctx, false); err != nil {
			t.Fatal(err)
		}
		if err := s.SetWorkListMode(ctx, settings.Rows); err != nil {
			t.Fatal(err)
		}
		if err := s.SetWorkSort(ctx, settings.ScheduledDate); err != nil {
			t.Fatal(err)
		}
		if err := s.SetShowFutureSchedules(ctx, visible); err != nil {
			t.Fatal(err)
		}
		if form, err := s.DisplaySettings(ctx); err != nil || form.RegistrationFormVisible {
			t.Fatal(form, err)
		}
		if mode, err := s.WorkListMode(ctx); err != nil || mode != settings.Rows {
			t.Fatal(mode, err)
		}
		if order, err := s.WorkSort(ctx); err != nil || order != settings.ScheduledDate {
			t.Fatal(order, err)
		}
		if got, err := s.ShowFutureSchedules(ctx); err != nil || got != visible {
			t.Fatal(got, err)
		}
	}
	// All older setters must preserve the new field in both directions.
	if err := s.SetRegistrationFormVisible(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetWorkListMode(ctx, settings.Compact); err != nil {
		t.Fatal(err)
	}
	if err := s.SetWorkSort(ctx, settings.TitleDesc); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ShowFutureSchedules(ctx); err != nil || got {
		t.Fatal(got, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, err := s.ShowFutureSchedules(ctx); err != nil || got {
		t.Fatal(got, err)
	}
	if got, err := s.WorkByID(ctx, w.ID); err != nil || !reflect.DeepEqual(got, w) {
		t.Fatal(got, err)
	}
}

func TestFutureSchedulesInvalidFailureAndStop(t *testing.T) {
	ctx := context.Background()
	s, _ := managedRecordStore(t)
	if err := s.SetShowFutureSchedules(ctx, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("UPDATE display_settings SET show_future_schedules=2"); err == nil {
		t.Fatal("missing bool CHECK")
	}
	if _, err := s.db.Exec("PRAGMA ignore_check_constraints=ON; UPDATE display_settings SET show_future_schedules=2; PRAGMA ignore_check_constraints=OFF"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ShowFutureSchedules(ctx); err == nil || !got {
		t.Fatal(got, err)
	}
	var raw int
	if err := s.db.QueryRow("SELECT show_future_schedules FROM display_settings").Scan(&raw); err != nil || raw != 2 {
		t.Fatal(raw, err)
	}
	if err := s.SetShowFutureSchedules(ctx, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("CREATE TRIGGER reject_future BEFORE UPDATE ON display_settings BEGIN SELECT RAISE(ABORT,'private diagnostic'); END"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetShowFutureSchedules(ctx, true); err == nil {
		t.Fatal("write accepted")
	}
	if got, err := s.ShowFutureSchedules(ctx); err != nil || got {
		t.Fatal(got, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.ShowFutureSchedules(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := s.SetShowFutureSchedules(cancelled, true); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("ALTER TABLE display_settings RENAME COLUMN show_future_schedules TO broken_future"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ShowFutureSchedules(ctx); err == nil {
		t.Fatal("missing column accepted")
	}
	if err := s.SetShowFutureSchedules(ctx, true); err == nil {
		t.Fatal("missing column accepted")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ShowFutureSchedules(ctx); !errors.Is(err, ErrDBStopped) {
		t.Fatal(err)
	}
	if err := s.SetShowFutureSchedules(ctx, true); !errors.Is(err, ErrDBStopped) {
		t.Fatal(err)
	}
}

func scheduleSettingsLegacyStore(t *testing.T) (*Store, string) {
	t.Helper()
	s, path := previousCoverSchema(t)
	for _, name := range []string{"004_managed_covers.sql", "005_work_list_mode.sql", "006_work_sort.sql", "007_scheduled_date.sql"} {
		if err := applyMigration(context.Background(), s.db, "migrations/"+name); err != nil {
			t.Fatal(err)
		}
	}
	return s, path
}

func TestScheduleSettingsMigrationPreservesData(t *testing.T) {
	for _, order := range []string{"title_desc", "unknown_future_order", ""} {
		t.Run(order, func(t *testing.T) {
			ctx := context.Background()
			old, path := scheduleSettingsLegacyStore(t)
			w := legacyCoverWork(t, old)
			w, err := old.EditWork(ctx, w.ID, dateEdit(w, manga.ScheduledDate("0001-01-01")), w.UpdatedAt.Add(time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			readyCover(t, old, testCoverID)
			cover, err := old.ManagedCoverByID(ctx, testCoverID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := old.db.Exec("PRAGMA ignore_check_constraints=ON; INSERT INTO display_settings VALUES (1, 0, 'rows', ?); PRAGMA ignore_check_constraints=OFF", order); err != nil {
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
				var actual string
				if err := s.db.QueryRow("SELECT work_sort FROM display_settings").Scan(&actual); err != nil || actual != order {
					t.Fatal(actual, err)
				}
				if got, err := s.WorkSort(ctx); order == "title_desc" && (err != nil || string(got) != order) || order != "title_desc" && !errors.Is(err, settings.ErrWorkSort) {
					t.Fatal(got, err)
				}
				if got, err := s.ShowFutureSchedules(ctx); err != nil || !got {
					t.Fatal(got, err)
				}
				if err := s.SetShowFutureSchedules(ctx, true); err != nil {
					t.Fatal(err)
				}
				if err := s.SetRegistrationFormVisible(ctx, false); err != nil {
					t.Fatal(err)
				}
				if err := s.SetWorkListMode(ctx, settings.Rows); err != nil {
					t.Fatal(err)
				}
				if err := s.db.QueryRow("SELECT work_sort FROM display_settings").Scan(&actual); err != nil || actual != order {
					t.Fatal(actual, err)
				}
				if got, err := s.DisplaySettings(ctx); err != nil || got.RegistrationFormVisible {
					t.Fatal(got, err)
				}
				if got, err := s.WorkListMode(ctx); err != nil || got != settings.Rows {
					t.Fatal(got, err)
				}
				if got, err := s.WorkByID(ctx, w.ID); err != nil || !reflect.DeepEqual(got, w) {
					t.Fatal(got, err)
				}
				if got, err := s.ManagedCoverByID(ctx, testCoverID); err != nil || !reflect.DeepEqual(got, cover) {
					t.Fatal(got, err)
				}
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestScheduleSettingsMigrationRollback(t *testing.T) {
	s, _ := scheduleSettingsLegacyStore(t)
	defer s.Close()
	if _, err := s.db.Exec("PRAGMA ignore_check_constraints=ON; INSERT INTO display_settings VALUES (1, 2, 'rows', 'title_desc'); PRAGMA ignore_check_constraints=OFF"); err != nil {
		t.Fatal(err)
	}
	if err := applyMigration(context.Background(), s.db, "migrations/008_schedule_settings.sql"); err == nil {
		t.Fatal("invalid legacy form accepted")
	}
	var count, visible int
	if err := s.db.QueryRow("SELECT registration_form_visible FROM display_settings").Scan(&visible); err != nil || visible != 2 {
		t.Fatal(visible, err)
	}
	if err := s.db.QueryRow("SELECT count(*) FROM schema_migrations WHERE version='008_schedule_settings.sql'").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	if err := s.db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name='new_display_settings'").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}

func TestScheduleSettingsMigrationEmptyAndLateRollback(t *testing.T) {
	ctx := context.Background()
	s, _ := scheduleSettingsLegacyStore(t)
	defer s.Close()
	// Fail after DROP/RENAME, when recording the migration, to prove atomicity.
	if _, err := s.db.Exec(`CREATE TRIGGER reject_schedule_migration BEFORE INSERT ON schema_migrations
 WHEN NEW.version='008_schedule_settings.sql' BEGIN SELECT RAISE(ABORT,'test'); END`); err != nil {
		t.Fatal(err)
	}
	if err := applyMigration(ctx, s.db, "migrations/008_schedule_settings.sql"); err == nil {
		t.Fatal("migration committed")
	}
	if _, err := s.db.Exec("INSERT INTO display_settings VALUES (1, 1, 'cards', 'unknown')"); err == nil {
		t.Fatal("old sort CHECK not restored")
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM schema_migrations WHERE version='008_schedule_settings.sql'").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	if _, err := s.db.Exec("DROP TRIGGER reject_schedule_migration"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := applyMigration(ctx, s.db, "migrations/008_schedule_settings.sql"); err != nil {
			t.Fatal(err)
		}
		if got, err := s.ShowFutureSchedules(ctx); err != nil || !got {
			t.Fatal(got, err)
		}
		if got, err := s.WorkSort(ctx); err != nil || got != settings.RegistrationAsc {
			t.Fatal(got, err)
		}
		if err := s.db.QueryRow("SELECT count(*) FROM display_settings").Scan(&count); err != nil || count != 0 {
			t.Fatal(count, err)
		}
	}
}
