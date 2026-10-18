package commitoutcome

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// This probe uses synthetic rows, not production Store or image operations.
func TestCommitOutcome(t *testing.T) {
	for _, mode := range []string{"success", "busy", "deferred-constraint"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			path := filepath.Join(t.TempDir(), "probe.sqlite")
			open := func() *sql.DB {
				db, err := sql.Open("sqlite", path)
				if err != nil {
					t.Fatal(err)
				}
				db.SetMaxOpenConns(1)
				t.Cleanup(func() {
					if err := db.Close(); err != nil {
						t.Error(err)
					}
				})
				return db
			}
			writer := open()
			exec := func(q string) {
				if _, err := writer.ExecContext(ctx, q); err != nil {
					t.Fatal(err)
				}
			}
			exec("PRAGMA journal_mode=DELETE")
			exec("PRAGMA busy_timeout=0")
			exec("PRAGMA foreign_keys=ON")
			exec("CREATE TABLE parent(id INTEGER PRIMARY KEY)")
			exec("INSERT INTO parent VALUES(1)")
			exec("CREATE TABLE item(id INTEGER PRIMARY KEY, parent_id INTEGER REFERENCES parent(id) DEFERRABLE INITIALLY DEFERRED)")
			exec("INSERT INTO item VALUES(1,1)")
			observer := open() // A separate pool means an independent SQLite connection.
			var read *sql.Tx
			if mode == "busy" {
				var err error
				read, err = observer.BeginTx(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer read.Rollback()
				var id int
				if err := read.QueryRowContext(ctx, "SELECT parent_id FROM item WHERE id=1").Scan(&id); err != nil || id != 1 {
					t.Fatalf("reader setup: id=%d err=%v", id, err)
				}
			}
			tx, err := writer.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			q := "INSERT INTO item VALUES(2,1)"
			if mode == "deferred-constraint" {
				q = "INSERT INTO item VALUES(2,99)"
			}
			if _, err := tx.ExecContext(ctx, q); err != nil {
				t.Fatal(err)
			}
			err = tx.Commit()
			if mode == "success" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				want := sqlite3.SQLITE_BUSY
				if mode == "deferred-constraint" {
					want = sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY
				}
				var e *sqlite.Error
				if !errors.As(err, &e) || e.Code() != want {
					t.Fatalf("commit: got %v, want code %d", err, want)
				}
			}
			if err := tx.Rollback(); !errors.Is(err, sql.ErrTxDone) {
				t.Fatalf("post-commit Rollback: %v", err)
			}
			if read != nil {
				if err := read.Rollback(); err != nil {
					t.Fatal(err)
				}
			}
			wantCount := 1
			if mode == "success" {
				wantCount = 2
			}
			check := func(db *sql.DB) {
				var n int
				if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM item").Scan(&n); err != nil || n != wantCount {
					t.Fatalf("persisted rows: got %d err=%v, want %d", n, err, wantCount)
				}
			}
			check(writer)
			check(observer)
			next, err := writer.BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("next transaction: %v", err)
			}
			if err := next.Rollback(); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			check(open()) // Reopen the file, not just the previous writer pool.
		})
	}
}
