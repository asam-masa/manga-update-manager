package storage

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/settings"
)

func preTagStore(t *testing.T) (*Store, string) {
	t.Helper()
	s, path := scheduleSettingsLegacyStore(t)
	if err := applyMigration(context.Background(), s.db, "migrations/008_schedule_settings.sql"); err != nil {
		t.Fatal(err)
	}
	return s, path
}

func TestTagsMigrationPreservesExistingData(t *testing.T) {
	ctx := context.Background()
	old, path := preTagStore(t)
	w := legacyCoverWork(t, old)
	w, err := old.EditWork(ctx, w.ID, dateEdit(w, manga.ScheduledDate("2028-02-29")), w.UpdatedAt.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
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
	if err := old.SetWorkSort(ctx, settings.ScheduledDate); err != nil {
		t.Fatal(err)
	}
	if err := old.SetShowFutureSchedules(ctx, false); err != nil {
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
		if got, err := s.ManagedCoverByID(ctx, testCoverID); err != nil || !reflect.DeepEqual(got, cover) {
			t.Fatal(got, err)
		}
		if got, err := s.DisplaySettings(ctx); err != nil || got.RegistrationFormVisible {
			t.Fatal(got, err)
		}
		if got, err := s.WorkListMode(ctx); err != nil || got != settings.Rows {
			t.Fatal(got, err)
		}
		if got, err := s.WorkSort(ctx); err != nil || got != settings.ScheduledDate {
			t.Fatal(got, err)
		}
		if got, err := s.ShowFutureSchedules(ctx); err != nil || got {
			t.Fatal(got, err)
		}
		var count int
		if err := s.db.QueryRow("SELECT (SELECT count(*) FROM tags)+(SELECT count(*) FROM work_tags)").Scan(&count); err != nil || count != 0 {
			t.Fatal(count, err)
		}
		if err := s.db.QueryRow("SELECT count(*) FROM schema_migrations WHERE version='009_tags.sql'").Scan(&count); err != nil || count != 1 {
			t.Fatal(count, err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTagsSchemaIdentityAndReferences(t *testing.T) {
	s, path := managedRecordStore(t)
	ctx := context.Background()
	w := coverReferenceWork(t, s)
	// This fixture explicitly enables FK enforcement. Production connections are
	// unchanged in this schema-only step; enabling every pooled connection is a
	// prerequisite for the later Store write operations.
	if _, err := s.db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	var enabled int
	if err := s.db.QueryRow("PRAGMA foreign_keys").Scan(&enabled); err != nil || enabled != 1 {
		t.Fatal(enabled, err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := s.db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	reject := func(query string, args ...any) {
		t.Helper()
		if _, err := s.db.Exec(query, args...); err == nil {
			t.Fatalf("accepted %s %v", query, args)
		}
	}
	exec("INSERT INTO tags(name) VALUES ('ABC'),('abc'),('ＡＢＣ'),('未使用')")
	exec("INSERT INTO tags(name) VALUES (?)", strings.Repeat("😀", 50))
	for _, name := range []string{"ABC", "", strings.Repeat("漫", 51), "a\x00b"} {
		reject("INSERT INTO tags(name) VALUES (?)", name)
	}
	reject("INSERT INTO tags(id,name) VALUES (0,'zero')")
	exec("INSERT INTO work_tags VALUES (?,1),(?,2)", w.ID, w.ID)
	reject("INSERT INTO work_tags VALUES (?,1)", w.ID)
	reject("INSERT INTO work_tags VALUES (?,999)", w.ID)
	reject("INSERT INTO work_tags VALUES (999,1)")
	reject("INSERT INTO work_tags VALUES (NULL,1)")
	exec("UPDATE tags SET name='renamed' WHERE id=1")
	exec("DELETE FROM tags WHERE id=2")
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM work_tags WHERE work_id=? AND tag_id=1", w.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if got, err := s.WorkByID(ctx, w.ID); err != nil || !reflect.DeepEqual(got, w) {
		t.Fatal(got, err)
	}
	if err := s.db.QueryRow("SELECT count(*) FROM work_tags WHERE tag_id=2").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	// The highest deleted ID must not become a different tag on automatic insert.
	exec("DELETE FROM tags WHERE id=5")
	result, err := s.db.Exec("INSERT INTO tags(name) VALUES ('new')")
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil || id <= 5 {
		t.Fatal(id, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var name string
	if err := s.db.QueryRow("SELECT name FROM tags WHERE id=1").Scan(&name); err != nil || name != "renamed" {
		t.Fatal(name, err)
	}
	if err := s.db.QueryRow("SELECT count(*) FROM work_tags WHERE work_id=? AND tag_id=1", w.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("reopened association", count, err)
	}
	if _, err := s.db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	// Test the production delete path under the FK-enabled connection, while
	// leaving the unused tags intact. No file cleanup is involved.
	if _, err := s.DeleteWork(ctx, w.ID, manga.WorkDeleteInput{URL: w.URL, Title: w.Title, SiteName: w.SiteName, Notes: w.Notes, ThumbnailPath: w.ThumbnailPath, ScheduledDate: w.ScheduledDate}); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("SELECT count(*) FROM work_tags").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	if err := s.db.QueryRow("SELECT count(*) FROM tags WHERE id IN (1,4)").Scan(&count); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	rows, err := s.db.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() || rows.Err() != nil {
		t.Fatal("foreign key violation", rows.Err())
	}
}

func TestTagsMigrationLateFailureRollsBack(t *testing.T) {
	s, _ := preTagStore(t)
	defer s.Close()
	w := legacyCoverWork(t, s)
	if _, err := s.db.Exec(`CREATE TRIGGER reject_tags_migration BEFORE INSERT ON schema_migrations
 WHEN NEW.version='009_tags.sql' BEGIN SELECT RAISE(ABORT,'test'); END`); err != nil {
		t.Fatal(err)
	}
	if err := applyMigration(context.Background(), s.db, "migrations/009_tags.sql"); err == nil {
		t.Fatal("migration committed")
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name IN ('tags','work_tags','work_tags_by_tag')").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	if err := s.db.QueryRow("SELECT count(*) FROM schema_migrations WHERE version='009_tags.sql'").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	if got, err := s.WorkByID(context.Background(), w.ID); err != nil || !reflect.DeepEqual(got, w) {
		t.Fatal(got, err)
	}
	if _, err := s.db.Exec("DROP TRIGGER reject_tags_migration"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := applyMigration(context.Background(), s.db, "migrations/009_tags.sql"); err != nil {
			t.Fatal(err)
		}
	}
}
