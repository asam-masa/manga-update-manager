package application

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/storage"
)

func TestRestoreComparisonStopsAfterRowsCloseFailure(t *testing.T) {
	p := restoreSQLTestOwner(strings.NewReader("fixture"), 7)
	registration := &restoreSQLTestCloser{}
	fault := errors.New("history rows close failure")
	queries, closes := 0, 0
	result, err := runRestoreSQL(context.Background(), p, restoreSQLOps{
		register: func(*restoreSQLFS) (string, io.Closer, error) { return "fixtureVFS", registration, nil },
		connect: func(string) (driver.Conn, error) {
			return restoreSQLTestConn{
				query: func() (driver.Rows, error) {
					queries++
					return restoreSQLTestRows{
						next:  func() error { return io.EOF },
						close: func() error { closes++; return fault },
					}, nil
				},
				close: func() error { return nil },
			}, nil
		},
		check: func(ctx context.Context, s *restoreSQLSession, r *restoreSQLResult) {
			s.compareRows(ctx, r, "first", nil, 0, 1, errRestoreMigrationHistory, nil)
			if r.ReleaseErr == nil {
				s.compareRows(ctx, r, "second", nil, 0, 1, errRestoreSchema, nil)
			}
		},
	})
	if !errors.Is(err, fault) || !errors.Is(err, ErrPreparedRestoreSQLReleaseUnconfirmed) ||
		result.Rows != restoreSQLUnknown || result.DB != restoreSQLUnknown ||
		queries != 1 || closes != 1 || registration.calls != 0 {
		t.Fatal(result, err, queries, closes, registration.calls)
	}
	if _, err := p.Cleanup(nil); !errors.Is(err, ErrPreparedRestoreSQLReleaseUnconfirmed) {
		t.Fatal(err)
	}
}

func TestPreparedRestoreSchema(t *testing.T) {
	ctx := context.Background()
	versions, _, err := storage.CurrentRestoreSchema(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, change string
		want         error
	}{
		{name: "fresh"},
		{name: "upgraded"},
		{name: "missing index", change: "DROP INDEX work_tags_by_tag", want: errRestoreSchema},
		{name: "extra object", change: "CREATE VIEW extra AS SELECT id FROM works", want: errRestoreSchema},
		{name: "future history", change: "UPDATE schema_migrations SET version='010_future.sql' WHERE version='009_tags.sql'", want: errRestoreMigrationHistory},
		{name: "missing history", change: "DELETE FROM schema_migrations WHERE version='009_tags.sql'", want: errRestoreMigrationHistory},
		{name: "manifest mismatch", want: errRestoreMigrationHistory},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "source.sqlite")
			if tc.name == "upgraded" {
				db, err := sql.Open("sqlite", source)
				if err != nil {
					t.Fatal(err)
				}
				old, err := os.ReadFile(filepath.Join("..", "storage", "migrations", versions[0]))
				if err != nil {
					db.Close()
					t.Fatal(err)
				}
				_, err = db.ExecContext(ctx, `CREATE TABLE schema_migrations (
    version TEXT PRIMARY KEY,
    applied_at TEXT NOT NULL
) STRICT;`+string(old)+`INSERT INTO schema_migrations VALUES ('001_create_works.sql', '2026-01-01T00:00:00Z');`)
				if err := errors.Join(err, db.Close()); err != nil {
					t.Fatal(err)
				}
			}
			store, err := storage.Open(ctx, source)
			if err != nil {
				t.Fatal(err)
			}
			copyPath := filepath.Join(dir, "copy.sqlite")
			if err := errors.Join(store.CopyDatabase(ctx, copyPath), store.Close()); err != nil {
				t.Fatal(err)
			}
			if tc.change != "" {
				db, err := sql.Open("sqlite", copyPath)
				if err != nil {
					t.Fatal(err)
				}
				_, err = db.ExecContext(ctx, tc.change)
				if err := errors.Join(err, db.Close()); err != nil {
					t.Fatal(err)
				}
			}
			data, err := os.ReadFile(copyPath)
			if err != nil {
				t.Fatal(err)
			}
			manifest := versions
			if tc.want == nil {
				manifest, err = storage.DatabaseCopyMigrations(ctx, copyPath)
				if err != nil {
					t.Fatal(err)
				}
			}
			if tc.name == "manifest mismatch" {
				manifest = versions[:1]
			}
			fixture := newRestoreFixtureWithMigrations(t, data, manifest)
			p, err := PrepareRestore(ctx, fixture.source, fixture.root)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := p.Cleanup(nil); err != nil {
					t.Error(err)
				}
				fixture.assertProtected(t)
			})
			result, err := checkPreparedRestoreSchema(ctx, p)
			if !errors.Is(err, tc.want) || result.ReleaseErr != nil {
				t.Fatal(result, err)
			}
		})
	}
}
