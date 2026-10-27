package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"path/filepath"
	"testing"
)

func TestForeignKeysOnEveryPhysicalConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connection.sqlite")
	// Caller pragmas are applied first; the Store hook must enforce its invariant.
	s, err := Open(context.Background(), path+"?_pragma=foreign_keys(0)")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for range 2 {
		conn, err := s.db.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		var enabled int
		if err := conn.QueryRowContext(context.Background(), "PRAGMA foreign_keys").Scan(&enabled); err != nil || enabled != 1 {
			t.Fatal(enabled, err)
		}
		if _, err := conn.ExecContext(context.Background(), "INSERT INTO work_tags VALUES (999,999)"); err == nil {
			t.Fatal("accepted orphan")
		}
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
		// Evict the idle connection, requiring the pool to open a new one next time.
		s.db.SetMaxIdleConns(0)
	}
	plain, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(0)")
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	var enabled int
	if err := plain.QueryRow("PRAGMA foreign_keys").Scan(&enabled); err != nil || enabled != 0 {
		t.Fatal("changed another driver's configuration", enabled, err)
	}
}

type foreignKeyProbe struct {
	value                                driver.Value
	execErr, queryErr, nextErr, closeErr error
}

func (p foreignKeyProbe) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return driver.RowsAffected(0), p.execErr
}
func (p foreignKeyProbe) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &foreignKeyRows{probe: p}, p.queryErr
}

type foreignKeyRows struct {
	probe foreignKeyProbe
	read  bool
}

func (r *foreignKeyRows) Columns() []string { return []string{"foreign_keys"} }
func (r *foreignKeyRows) Close() error      { return r.probe.closeErr }
func (r *foreignKeyRows) Next(values []driver.Value) error {
	if r.probe.nextErr != nil {
		return r.probe.nextErr
	}
	if r.read {
		return io.EOF
	}
	r.read = true
	values[0] = r.probe.value
	return nil
}
func TestForeignKeyHookRejectsUnverifiedConnections(t *testing.T) {
	fault := errors.New("synthetic hook failure")
	for _, p := range []foreignKeyProbe{{execErr: fault}, {queryErr: fault}, {nextErr: fault}, {value: int64(1), closeErr: fault}, {value: int64(0)}, {value: "1"}} {
		if err := enableForeignKeys(p, ""); err == nil {
			t.Fatal("accepted unverified connection", p)
		}
	}
	if err := enableForeignKeys(foreignKeyProbe{value: int64(1)}, ""); err != nil {
		t.Fatal(err)
	}
}
