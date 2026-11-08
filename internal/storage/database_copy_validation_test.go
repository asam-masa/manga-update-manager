package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"modernc.org/sqlite"
)

func TestValidateDatabaseCopyReadOnlyAndPreserves(t *testing.T) {
	for _, mode := range []string{"valid", "foreign-key", "broken"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "コピー # 100%.sqlite")
			if mode == "broken" {
				if err := os.WriteFile(path, []byte("not a SQLite database"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				db, err := sql.Open("sqlite", path)
				if err != nil {
					t.Fatal(err)
				}
				_, execErr := db.Exec("CREATE TABLE parent(id INTEGER PRIMARY KEY); CREATE TABLE child(parent_id INTEGER REFERENCES parent(id)); INSERT INTO parent VALUES(1); INSERT INTO child VALUES(1)")
				if mode == "foreign-key" && execErr == nil {
					_, execErr = db.Exec("INSERT INTO child VALUES(999)")
				}
				if err := errors.Join(execErr, db.Close()); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			err = ValidateDatabaseCopy(bounded(t), path)
			if (err == nil) != (mode == "valid") || err != nil && !errors.Is(err, ErrDatabaseCopyValidation) {
				t.Fatal(err)
			}
			if errors.Is(err, ErrDatabaseCopyRelease) {
				t.Fatal("ordinary validation rejection marked unreleased", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || sha256.Sum256(after) != sha256.Sum256(before) {
				t.Fatal("copy changed", err)
			}
			dsn, err := databaseCopyReadOnlyDSN(path)
			if err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", dsn)
			if err != nil {
				t.Fatal(err)
			}
			_, writeErr := db.Exec("CREATE TABLE unexpected(id INTEGER)")
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			if writeErr == nil {
				t.Fatal("read-only DSN allowed write")
			}
		})
	}
}

func TestValidateDatabaseCopyMissingAndCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.sqlite")
	if err := ValidateDatabaseCopy(bounded(t), path); !errors.Is(err, ErrDatabaseCopyValidation) {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("missing DB created", err)
	}
	ctx, cancel := context.WithCancel(bounded(t))
	cancel()
	if err := ValidateDatabaseCopy(ctx, path); !errors.Is(err, context.Canceled) || errors.Is(err, ErrDatabaseCopyRelease) {
		t.Fatal(err)
	}
	for _, path := range []string{"", "relative.sqlite", "file:copy.sqlite?mode=rw"} {
		if err := ValidateDatabaseCopy(bounded(t), path); !errors.Is(err, ErrDatabaseCopyValidation) {
			t.Fatal(err)
		}
	}
}

var validationCloseFault = errors.New("validation DB close failed")

type validationCloseDriver struct{ cancel func() }

func (d validationCloseDriver) Open(name string) (driver.Conn, error) {
	c, err := (&sqlite.Driver{}).Open(name)
	if err != nil {
		return nil, err
	}
	return validationCloseConn{Conn: c, cancel: d.cancel}, nil
}

type validationCloseConn struct {
	driver.Conn
	cancel func()
}

func (c validationCloseConn) Close() error {
	err := c.Conn.Close()
	if c.cancel != nil {
		c.cancel()
		return err
	}
	return errors.Join(err, validationCloseFault)
}

func TestValidateDatabaseCopyCancellationDuringClose(t *testing.T) {
	_, source := managedRecordStore(t)
	ctx, cancel := context.WithCancel(bounded(t))
	defer cancel()
	name := fmt.Sprintf("validation-cancel-close-%d", faultDriverID.Add(1))
	sql.Register(name, validationCloseDriver{cancel: cancel})
	db, err := sql.Open(name, source)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateDatabaseCopy(ctx, db); !errors.Is(err, context.Canceled) || errors.Is(err, ErrDatabaseCopyRelease) {
		t.Fatal("cancellation lost", err)
	}
}

func TestValidateDatabaseCopyCleanupFaults(t *testing.T) {
	_, source := managedRecordStore(t)
	for _, mode := range []string{"rows", "rollback", "connection"} {
		t.Run(mode, func(t *testing.T) {
			name := fmt.Sprintf("copy-validation-%d", faultDriverID.Add(1))
			if mode == "connection" {
				sql.Register(name, validationCloseDriver{})
			} else {
				sql.Register(name, cleanupDriver{rows: mode == "rows", commit: mode == "rows"})
			}
			db, err := sql.Open(name, source)
			if err != nil {
				t.Fatal(err)
			}
			db.SetMaxOpenConns(1)
			err = validateDatabaseCopy(bounded(t), db)
			cause := syntheticCleanup
			if mode == "connection" {
				cause = validationCloseFault
			}
			if !errors.Is(err, cause) || !errors.Is(err, ErrDatabaseCopyRelease) {
				t.Fatal("cleanup failure hidden", err)
			}
			if err := db.Ping(); err == nil {
				t.Fatal("database ownership leaked")
			}
		})
	}
}

type validationResultDriver struct {
	integrity []string
	foreign   []string
	nextErr   error
}

func (d validationResultDriver) Open(name string) (driver.Conn, error) {
	c, err := (&sqlite.Driver{}).Open(name)
	if err != nil {
		return nil, err
	}
	return validationResultConn{Conn: c, results: d}, nil
}

type validationResultConn struct {
	driver.Conn
	results validationResultDriver
}

func (c validationResultConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	if q == "PRAGMA integrity_check" {
		return &validationResultRows{values: append([]string(nil), c.results.integrity...), nextErr: c.results.nextErr}, nil
	}
	if q == "PRAGMA foreign_key_check" {
		return &validationResultRows{values: append([]string(nil), c.results.foreign...)}, nil
	}
	return nil, errors.New("unexpected validation query")
}

type validationResultRows struct {
	values  []string
	nextErr error
}

func (*validationResultRows) Columns() []string { return []string{"result"} }
func (*validationResultRows) Close() error      { return nil }
func (r *validationResultRows) Next(out []driver.Value) error {
	if len(r.values) == 0 {
		if r.nextErr != nil {
			err := r.nextErr
			r.nextErr = nil
			return err
		}
		return io.EOF
	}
	out[0] = r.values[0]
	r.values = r.values[1:]
	return nil
}
func TestValidateDatabaseCopyRequiresExactResults(t *testing.T) {
	_, source := managedRecordStore(t)
	for _, tc := range []validationResultDriver{{}, {integrity: []string{"damaged"}}, {integrity: []string{"ok", "ok"}}, {integrity: []string{"ok"}, foreign: []string{"violation"}}} {
		name := fmt.Sprintf("validation-result-%d", faultDriverID.Add(1))
		sql.Register(name, tc)
		db, err := sql.Open(name, source)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateDatabaseCopy(bounded(t), db); !errors.Is(err, ErrDatabaseCopyValidation) || errors.Is(err, ErrDatabaseCopyRelease) {
			t.Fatal("invalid results accepted", tc, err)
		}
	}
}

func TestValidateDatabaseCopyAmbiguousIterationStopsRelease(t *testing.T) {
	_, path := managedRecordStore(t)
	cause := errors.New("synthetic iteration failure with successful Close")
	name := fmt.Sprintf("copy-iteration-%d", faultDriverID.Add(1))
	sql.Register(name, validationResultDriver{integrity: []string{"ok"}, nextErr: cause})
	db, err := sql.Open(name, path)
	if err != nil {
		t.Fatal(err)
	}
	err = validateDatabaseCopy(bounded(t), db)
	if !errors.Is(err, cause) || !errors.Is(err, ErrDatabaseCopyRelease) {
		t.Fatal("ambiguous iteration lost", err)
	}
	if err := db.Ping(); err == nil {
		t.Fatal("DB not closed")
	}
}
