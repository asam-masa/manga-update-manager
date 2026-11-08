package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"testing"

	"modernc.org/sqlite"
)

func TestDatabaseCopyMigrationsCurrentAndReadOnly(t *testing.T) {
	s, source := managedRecordStore(t)
	copyPath := filepath.Join(t.TempDir(), "コピー # 100%.sqlite")
	if err := s.CopyDatabase(bounded(t), copyPath); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("DELETE FROM schema_migrations"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	before := map[string][32]byte{}
	for _, p := range []string{source, copyPath} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		before[p] = sha256.Sum256(b)
	}
	want, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for i := range want {
		want[i] = path.Base(want[i])
	}
	got, err := DatabaseCopyMigrations(bounded(t), copyPath)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, err)
	}
	for p, hash := range before {
		b, err := os.ReadFile(p)
		if err != nil || sha256.Sum256(b) != hash {
			t.Fatal("database changed", p, err)
		}
	}
}

func TestDatabaseCopyMigrationsRejectsHistory(t *testing.T) {
	for name, mutation := range map[string]string{
		"empty":     "DELETE FROM schema_migrations",
		"old":       "DELETE FROM schema_migrations WHERE version='009_tags.sql'",
		"gap":       "DELETE FROM schema_migrations WHERE version='002_last_access.sql'",
		"unknown":   "UPDATE schema_migrations SET version='002_unknown.sql' WHERE version='002_last_access.sql'",
		"future":    "INSERT INTO schema_migrations VALUES('010_future.sql','synthetic')",
		"missing":   "DROP TABLE schema_migrations",
		"duplicate": "CREATE TABLE history AS SELECT * FROM schema_migrations; DROP TABLE schema_migrations; ALTER TABLE history RENAME TO schema_migrations; INSERT INTO schema_migrations SELECT * FROM schema_migrations LIMIT 1",
		"null":      "DROP TABLE schema_migrations; CREATE TABLE schema_migrations(version); INSERT INTO schema_migrations VALUES(NULL)",
		"blob":      "DROP TABLE schema_migrations; CREATE TABLE schema_migrations(version); INSERT INTO schema_migrations VALUES(CAST('001_create_works.sql' AS BLOB))",
		"oversized": "UPDATE schema_migrations SET version=hex(zeroblob(100000)) WHERE version='009_tags.sql'",
	} {
		t.Run(name, func(t *testing.T) {
			s, copyPath := managedRecordStore(t)
			if _, err := s.db.Exec(mutation); err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			got, err := DatabaseCopyMigrations(bounded(t), copyPath)
			if got != nil || !errors.Is(err, ErrDatabaseCopyMigrations) || errors.Is(err, ErrDatabaseCopyRelease) {
				t.Fatal(got, err)
			}
		})
	}
}

func TestDatabaseCopyMigrationsMissingPathAndCancellation(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.sqlite")
	for _, p := range []string{missing, "", "relative.sqlite", "file:copy.sqlite?mode=rw", "bad\x00path"} {
		got, err := DatabaseCopyMigrations(bounded(t), p)
		if got != nil || !errors.Is(err, ErrDatabaseCopyMigrations) || errors.Is(err, ErrDatabaseCopyRelease) {
			t.Fatal(got, err)
		}
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("created missing copy", err)
	}
	ctx, cancel := context.WithCancel(bounded(t))
	cancel()
	got, err := DatabaseCopyMigrations(ctx, missing)
	if got != nil || !errors.Is(err, context.Canceled) || errors.Is(err, ErrDatabaseCopyRelease) {
		t.Fatal(got, err)
	}
}

func TestDatabaseCopyMigrationsCleanupAndIterationFaults(t *testing.T) {
	_, copyPath := managedRecordStore(t)
	for _, mode := range []string{"rows", "rollback", "connection", "iteration", "cancel-close"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(bounded(t))
			defer cancel()
			name := fmt.Sprintf("copy-migrations-%d", faultDriverID.Add(1))
			cause := syntheticCleanup
			switch mode {
			case "connection":
				sql.Register(name, validationCloseDriver{})
				cause = validationCloseFault
			case "cancel-close":
				sql.Register(name, validationCloseDriver{cancel: cancel})
				cause = context.Canceled
			case "iteration":
				sql.Register(name, migrationReadFaultDriver{})
			default:
				sql.Register(name, cleanupDriver{rows: mode == "rows", commit: mode == "rows"})
			}
			db, err := sql.Open(name, copyPath)
			if err != nil {
				t.Fatal(err)
			}
			db.SetMaxOpenConns(1)
			got, err := readDatabaseCopyMigrations(ctx, db)
			if got != nil || !errors.Is(err, cause) || errors.Is(err, ErrDatabaseCopyRelease) != (mode != "cancel-close") {
				t.Fatal(got, err)
			}
			if db.Ping() == nil {
				t.Fatal("connection retained")
			}
		})
	}
}

type migrationReadFaultDriver struct{}

func (migrationReadFaultDriver) Open(name string) (driver.Conn, error) {
	c, err := (&sqlite.Driver{}).Open(name)
	if err != nil {
		return nil, err
	}
	return migrationReadFaultConn{Conn: c}, nil
}

type migrationReadFaultConn struct{ driver.Conn }

func (migrationReadFaultConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &validationResultRows{values: []string{"001_create_works.sql"}, nextErr: syntheticCleanup}, nil
}
