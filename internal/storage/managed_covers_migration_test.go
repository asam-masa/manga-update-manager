package storage

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
)

const testCoverID = "0123456789abcdef0123456789abcdef"

// Build the actual pre-004 schema from its unchanged embedded SQL files.
func previousCoverSchema(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pre-cover.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL) STRICT`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"001_create_works.sql", "002_last_access.sql", "003_display_settings.sql"} {
		if err := applyMigration(context.Background(), db, "migrations/"+name); err != nil {
			t.Fatal(err)
		}
	}
	return &Store{db: db}, path
}

// Migration fixtures write the actual legacy columns, never today's Store API.
func legacyCreateWork(t *testing.T, s *Store, work manga.Work) manga.Work {
	t.Helper()
	result, err := s.db.Exec(`INSERT INTO works(url,title,site_name,thumbnail_path,notes,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
		work.URL, work.Title, work.SiteName, work.ThumbnailPath, work.Notes, formatTime(work.CreatedAt), formatTime(work.UpdatedAt))
	if err != nil {
		t.Fatal(err)
	}
	work.ID, err = result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return work
}

func legacyWorkByID(t *testing.T, s *Store, id int64) manga.Work {
	t.Helper()
	work, err := scanWork(queryOne(context.Background(), s.db, `SELECT id,url,title,site_name,thumbnail_path,notes,created_at,updated_at,last_accessed_at,NULL FROM works WHERE id=?`, id))
	if err != nil {
		t.Fatal(err)
	}
	return work
}

func legacyCoverWork(t *testing.T, s *Store) manga.Work {
	t.Helper()
	w, err := manga.NewWork(manga.NewWorkInput{URL: "https://example.com/cover-test", Title: "Synthetic", Notes: "unchanged", ThumbnailPath: "legacy/image.png"}, time.Date(2026, 10, 13, 1, 2, 3, 123456789, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	w = legacyCreateWork(t, s, w)
	if err := s.RecordLastAccess(context.Background(), w.ID, w.CreatedAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	return legacyWorkByID(t, s, w.ID)
}

func TestManagedCoverMigrationPreservesExistingData(t *testing.T) {
	ctx := context.Background()
	old, path := previousCoverSchema(t)
	work, err := manga.NewWork(manga.NewWorkInput{URL: "https://example.com/legacy", Title: "既存作品", ThumbnailPath: "covers/legacy.png", Notes: "保持するメモ"}, time.Date(2026, 10, 1, 0, 0, 0, 123, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	work = legacyCreateWork(t, old, work)
	if err := old.RecordLastAccess(ctx, work.ID, work.CreatedAt.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := old.SetRegistrationFormVisible(ctx, false); err != nil {
		t.Fatal(err)
	}
	before := legacyWorkByID(t, old, work.ID)
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	after, err := store.WorkByID(ctx, work.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("work changed: %#v, %v", after, err)
	}
	settings, err := store.DisplaySettings(ctx)
	if err != nil || settings.RegistrationFormVisible {
		t.Fatalf("settings changed: %#v, %v", settings, err)
	}
	var count int
	if err := store.db.QueryRow("SELECT count(*) FROM managed_covers").Scan(&count); err != nil || count != 0 {
		t.Fatalf("legacy paths enrolled: %d %v", count, err)
	}
	if err := store.db.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count != 9 {
		t.Fatalf("migration count: %d %v", count, err)
	}
	var integrity string
	if err := store.db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity: %q %v", integrity, err)
	}
}

func TestManagedCoverSchemaConstraintsAndReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "managed.sqlite")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if store != nil {
			store.Close()
		}
	})
	if _, err := store.db.Exec("INSERT INTO managed_covers(id,state) VALUES (?,'reserved')", testCoverID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []any{nil, "", strings.Repeat("a", 31), strings.Repeat("a", 33), strings.Repeat("A", 32), strings.Repeat("z", 32), testCoverID + "\x00tail", testCoverID} {
		if _, err := store.db.Exec("INSERT INTO managed_covers(id,state) VALUES (?,'reserved')", id); err == nil {
			t.Fatalf("invalid or duplicate ID accepted: %q", id)
		}
	}
	for _, state := range []any{nil, "", "unknown", "ready"} {
		if _, err := store.db.Exec("INSERT INTO managed_covers(id,state) VALUES (?,?)", strings.Repeat("a", 32), state); err == nil {
			t.Fatalf("invalid state accepted: %v", state)
		}
	}
	if _, err := store.db.Exec("UPDATE managed_covers SET relative_path='outside.png'"); err == nil {
		t.Fatal("generated path overwritten")
	}
	if _, err := store.db.Exec("INSERT INTO managed_covers(id,state,relative_path) VALUES (?,'reserved','outside.png')", strings.Repeat("a", 32)); err == nil {
		t.Fatal("arbitrary path inserted")
	}
	volume, fileID, hash := strings.Repeat("f", 16), strings.Repeat("a", 32), bytes.Repeat([]byte{0xff}, 32)
	for _, tc := range []struct {
		name                       string
		volume, fileID, size, hash any
	}{
		{"missing volume", nil, fileID, 1, hash}, {"missing ID", volume, nil, 1, hash},
		{"missing size", volume, fileID, nil, hash}, {"missing hash", volume, fileID, 1, nil},
		{"old volume", strings.Repeat("a", 8), fileID, 1, hash}, {"old ID", volume, strings.Repeat("a", 16), 1, hash},
		{"NUL volume", volume + "\x00tail", fileID, 1, hash}, {"NUL ID", volume, fileID + "\x00tail", 1, hash},
		{"invalid volume", strings.Repeat("g", 16), fileID, 1, hash}, {"invalid ID", volume, strings.Repeat("G", 32), 1, hash},
		{"zero size", volume, fileID, 0, hash}, {"negative size", volume, fileID, -1, hash},
		{"fractional size", volume, fileID, 1.5, hash}, {"text size", volume, fileID, "invalid", hash},
		{"short hash", volume, fileID, 1, hash[:31]}, {"long hash", volume, fileID, 1, append(append([]byte{}, hash...), 0)},
		{"text hash", volume, fileID, 1, strings.Repeat("f", 32)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := store.db.Exec("UPDATE managed_covers SET state='ready',volume_serial=?,file_id=?,byte_size=?,sha256=? WHERE id=?", tc.volume, tc.fileID, tc.size, tc.hash, testCoverID); err == nil {
				t.Fatal("invalid ready state accepted")
			}
			var reserved bool
			if err := store.db.QueryRow("SELECT state='reserved' AND volume_serial IS NULL AND file_id IS NULL AND byte_size IS NULL AND sha256 IS NULL FROM managed_covers WHERE id=?", testCoverID).Scan(&reserved); err != nil || !reserved {
				t.Fatalf("partial update: %v %v", reserved, err)
			}
		})
	}
	for _, field := range []struct {
		column string
		value  any
	}{{"volume_serial", volume}, {"file_id", fileID}, {"byte_size", 1}, {"sha256", hash}} {
		if _, err := store.db.Exec("UPDATE managed_covers SET "+field.column+"=? WHERE id=?", field.value, testCoverID); err == nil {
			t.Fatalf("reserved contains %s", field.column)
		}
	}
	if _, err := store.db.Exec("UPDATE managed_covers SET state='ready',volume_serial=?,file_id=?,byte_size=123,sha256=? WHERE id=?", volume, fileID, hash, testCoverID); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	var gotPath, state, gotVolume, gotID string
	var size int64
	var gotHash []byte
	if err := store.db.QueryRow("SELECT relative_path,state,volume_serial,file_id,byte_size,sha256 FROM managed_covers WHERE id=?", testCoverID).Scan(&gotPath, &state, &gotVolume, &gotID, &size, &gotHash); err != nil {
		t.Fatal(err)
	}
	if gotPath != "covers/"+testCoverID+".png" || state != "ready" || gotVolume != volume || gotID != fileID || size != 123 || !bytes.Equal(gotHash, hash) {
		t.Fatal("managed record changed after reopen")
	}
	var count int
	if err := store.db.QueryRow("SELECT count(*) FROM schema_migrations WHERE version='004_managed_covers.sql'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("migration reapplied: %d %v", count, err)
	}
}

func TestManagedCoverMigrationRecordFailureRollsBackTable(t *testing.T) {
	ctx := context.Background()
	old, path := previousCoverSchema(t)
	if _, err := old.db.Exec(`CREATE TRIGGER reject_cover_migration BEFORE INSERT ON schema_migrations
 WHEN NEW.version='004_managed_covers.sql' BEGIN SELECT RAISE(ABORT,'synthetic migration record failure'); END;`); err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	if store, err := Open(ctx, path); err == nil {
		store.Close()
		t.Fatal("migration record failure accepted")
	} else if !strings.Contains(err.Error(), "synthetic migration record failure") {
		t.Fatalf("unexpected error: %v", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name='managed_covers'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("table not rolled back: %d %v", count, err)
	}
	if err := db.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count != 3 {
		t.Fatalf("migration records changed: %d %v", count, err)
	}
	if _, err := db.Exec("DROP TRIGGER reject_cover_migration"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
}
