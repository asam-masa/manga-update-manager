package storage

import (
	"context"
	"database/sql"
	"errors"
	"github.com/asam-masa/manga-update-manager/internal/manga"
	"path/filepath"
	"testing"
	"time"
)

func TestAccessMigrationAndPersistence(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "migration.sqlite")
	// Start with the real version-001 schema and an existing work.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := migrationFiles.ReadFile("migrations/001_create_works.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(schema)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("CREATE TABLE schema_migrations(version TEXT PRIMARY KEY, applied_at TEXT NOT NULL); INSERT INTO schema_migrations VALUES ('001_create_works.sql', '2026-10-01T00:00:00Z'); INSERT INTO works(url,title,site_name,thumbnail_path,notes,created_at,updated_at) VALUES ('https://example.com','作品','','','','2026-10-01T00:00:00.000000000Z','2026-10-01T00:00:00.000000000Z')"); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	w, err := s.WorkByID(ctx, 1)
	if err != nil || w.LastAccessedAt != nil {
		t.Fatal(w, err)
	}
	at := time.Date(2026, 10, 9, 12, 0, 0, 123, time.FixedZone("JST", 9*3600))
	for _, stamp := range []time.Time{at.Add(-time.Hour), at} {
		if err = s.RecordLastAccess(ctx, 1, stamp); err != nil {
			t.Fatal(err)
		}
	}
	var raw string
	if err = s.db.QueryRow("SELECT last_accessed_at FROM works WHERE id=1").Scan(&raw); err != nil || raw != "2026-10-09T03:00:00.000000123Z" {
		t.Fatal(raw, err)
	}
	s.Close()
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.ListWorks(ctx)
	if err != nil || len(list) != 1 || list[0].LastAccessedAt == nil || !list[0].LastAccessedAt.Equal(at) || !list[0].UpdatedAt.Equal(w.UpdatedAt) {
		t.Fatal(list, err)
	}
	if err = s.RecordLastAccess(ctx, 99, at); !errors.Is(err, manga.ErrWorkNotFound) {
		t.Fatal(err)
	}
	if err = s.RecordLastAccess(ctx, 1, time.Time{}); !errors.Is(err, manga.ErrTimestampInvalid) {
		t.Fatal(err)
	}
}
