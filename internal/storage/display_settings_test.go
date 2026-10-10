package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
)

func TestDisplaySettingsPersistenceAndNoImplicitWrite(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "settings.sqlite")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	value, err := store.DisplaySettings(ctx)
	if err != nil || !value.RegistrationFormVisible {
		t.Fatalf("default = %#v, %v", value, err)
	}
	var count int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM display_settings").Scan(&count); err != nil || count != 0 {
		t.Fatalf("implicit write: %d, %v", count, err)
	}
	if err := store.SetRegistrationFormVisible(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	value, err = store.DisplaySettings(ctx)
	if err != nil || value.RegistrationFormVisible {
		t.Fatalf("reopen = %#v, %v", value, err)
	}
	if err := store.SetRegistrationFormVisible(ctx, true); err != nil {
		t.Fatal(err)
	}
	value, err = store.DisplaySettings(ctx)
	if err != nil || !value.RegistrationFormVisible {
		t.Fatalf("updated = %#v, %v", value, err)
	}
}

func TestInvalidDisplaySettingsAreNotRepaired(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "settings.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// Simulate damaged data in an isolated test database, bypassing its CHECK only here.
	if _, err := store.db.Exec("PRAGMA ignore_check_constraints = ON; INSERT INTO display_settings VALUES (1, 2); PRAGMA ignore_check_constraints = OFF;"); err != nil {
		t.Fatal(err)
	}
	value, err := store.DisplaySettings(ctx)
	if err == nil || !value.RegistrationFormVisible {
		t.Fatalf("invalid = %#v, %v", value, err)
	}
	var raw int
	if err := store.db.QueryRow("SELECT registration_form_visible FROM display_settings").Scan(&raw); err != nil || raw != 2 {
		t.Fatalf("unexpected repair = %d, %v", raw, err)
	}
	if err := store.SetRegistrationFormVisible(ctx, false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec("UPDATE display_settings SET registration_form_visible = 2"); err == nil {
		t.Fatal("CHECK must reject unknown values")
	}
	if _, err := store.db.Exec("INSERT INTO display_settings VALUES (2, 1)"); err == nil {
		t.Fatal("only singleton row is allowed")
	}
}

func TestDisplaySettingsFailurePreservesStoredValueAndWorks(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "settings.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	work, err := manga.NewWork(manga.NewWorkInput{URL: "https://example.com/test", Title: "作品"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.CreateWork(ctx, work)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetRegistrationFormVisible(ctx, false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec("CREATE TRIGGER fail_settings BEFORE UPDATE ON display_settings BEGIN SELECT RAISE(ABORT, 'private diagnostic'); END;"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRegistrationFormVisible(ctx, true); err == nil {
		t.Fatal("write must fail")
	}
	value, err := store.DisplaySettings(ctx)
	if err != nil || value.RegistrationFormVisible {
		t.Fatalf("stored value changed: %#v, %v", value, err)
	}
	got, err := store.WorkByID(ctx, created.ID)
	if err != nil || got != created {
		t.Fatalf("work changed: %#v, %v", got, err)
	}
	store.Close()
	if _, err := store.DisplaySettings(ctx); err == nil {
		t.Fatal("closed database read must fail")
	}
	if err := store.SetRegistrationFormVisible(ctx, true); err == nil {
		t.Fatal("closed database write must fail")
	}
}

func TestDisplaySettingsMigrationPreservesExistingWorks(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.sqlite")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	work, err := manga.NewWork(manga.NewWorkInput{URL: "https://example.com/old", Title: "既存作品"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.CreateWork(ctx, work)
	if err != nil {
		t.Fatal(err)
	}
	// Restore this temporary fixture to the two-migration schema used before #27.
	if _, err := store.db.Exec("DROP TABLE display_settings; DELETE FROM schema_migrations WHERE version = '003_display_settings.sql';"); err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := store.WorkByID(ctx, created.ID)
	if err != nil || got != created {
		t.Fatalf("migration changed work: %#v, %v", got, err)
	}
	value, err := store.DisplaySettings(ctx)
	if err != nil || !value.RegistrationFormVisible {
		t.Fatalf("migrated default: %#v, %v", value, err)
	}
}

func TestUnreadableSettingsDoNotBlockWorks(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "settings.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.db.Exec("ALTER TABLE display_settings RENAME COLUMN registration_form_visible TO broken_column"); err != nil {
		t.Fatal(err)
	}
	value, err := store.DisplaySettings(ctx)
	if err == nil || !value.RegistrationFormVisible {
		t.Fatalf("unreadable = %#v, %v", value, err)
	}
	work, err := manga.NewWork(manga.NewWorkInput{URL: "https://example.com/available", Title: "保存できる作品"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.CreateWork(ctx, work)
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.WorkByID(ctx, created.ID)
	if err != nil || got != created {
		t.Fatalf("work unavailable: %#v, %v", got, err)
	}
}

func TestDisplaySettingsSavePreservesUnrelatedColumns(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "settings.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.db.Exec("ALTER TABLE display_settings ADD COLUMN unrelated TEXT NOT NULL DEFAULT 'keep';"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRegistrationFormVisible(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRegistrationFormVisible(ctx, true); err != nil {
		t.Fatal(err)
	}
	var unrelated string
	if err := store.db.QueryRow("SELECT unrelated FROM display_settings WHERE id = 1").Scan(&unrelated); err != nil || unrelated != "keep" {
		t.Fatalf("unrelated = %q, %v", unrelated, err)
	}
}
