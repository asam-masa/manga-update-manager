package storage

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"modernc.org/sqlite"
)

func TestCopyCoverWorksSnapshot(t *testing.T) {
	s, _ := managedRecordStore(t)
	a, b := strings.Repeat("a", 32), strings.Repeat("b", 32)
	for _, id := range []string{a, b} {
		readyCover(t, s, id)
	}
	for _, id := range []string{b, a, a} {
		copyCoverWork(t, s, "covers/"+id+".png")
	}
	copyCoverWork(t, s, "")
	path := filepath.Join(t.TempDir(), "コピー # %.sqlite")
	if err := s.CopyDatabase(bounded(t), path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("UPDATE works SET title='変更後'"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DatabaseCopyCoverWorks(bounded(t), path, []string{b, a})
	if err != nil || len(got) != 3 {
		t.Fatal(got, err)
	}
	for i, want := range []CopyCoverWork{{a, 2, "合成作品"}, {a, 3, "合成作品"}, {b, 1, "合成作品"}} {
		if got[i] != want {
			t.Fatal(got)
		}
	}
	got, err = DatabaseCopyCoverWorks(bounded(t), path, []string{b})
	if err != nil || len(got) != 1 || got[0].WorkID != 1 {
		t.Fatal(got, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("reader changed the copy", err)
	}
}

// Batch inserts keep the production-size boundaries practical without relaxing
// the reader's actual limits. All databases and images here are synthetic.
func detailFixture(t *testing.T, count int, title string) (*Store, string) {
	t.Helper()
	s, path := managedRecordStore(t)
	readyCover(t, s, testCoverID)
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT INTO works(url,title,thumbnail_path,created_at,updated_at) VALUES(?,?,?,'2026-01-01T00:00:00.000000000Z','2026-01-01T00:00:00.000000000Z')`)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	for i := range count {
		if _, err := stmt.Exec(fmt.Sprintf("https://example.com/detail/%d", i), title, "covers/"+testCoverID+".png"); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return s, path
}

func TestCopyCoverWorksByteBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name  string
		count int
		title string
		limit bool
	}{
		{"single-below", 1, strings.Repeat("x", 4095), false},
		{"single-exact", 1, strings.Repeat("あ", 1365) + "x", false},
		{"single-over", 1, strings.Repeat("x", 4097), true},
		{"nul-over", 1, "x\x00" + strings.Repeat("あ", 1365), true},
		{"total-below", 2048, strings.Repeat("x", 4096), false},
		{"total-exact", 2048, strings.Repeat("x", 4096), false},
		{"total-over", 2049, strings.Repeat("x", 4096), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, path := detailFixture(t, tc.count, tc.title)
			if tc.name == "total-below" || tc.name == "total-over" {
				last := "x"
				if tc.name == "total-below" {
					last = strings.Repeat("x", 4095)
				}
				if _, err := s.db.Exec("UPDATE works SET title=? WHERE id=?", last, tc.count); err != nil {
					t.Fatal(err)
				}
			}
			got, err := DatabaseCopyCoverWorks(bounded(t), path, []string{testCoverID})
			if tc.limit {
				if got != nil || !errors.Is(err, ErrCopyCoverWorkLimit) || errors.Is(err, ErrDatabaseCopyRelease) {
					t.Fatal(len(got), err)
				}
			} else if err != nil || len(got) != tc.count || got[0].Title != tc.title {
				t.Fatal(len(got), err)
			}
		})
	}
}

func TestCopyCoverWorksCountBoundaries(t *testing.T) {
	s, path := detailFixture(t, MaxCopyCoverWorks-1, "題")
	for _, count := range []int{MaxCopyCoverWorks - 1, MaxCopyCoverWorks, MaxCopyCoverWorks + 1} {
		got, err := DatabaseCopyCoverWorks(bounded(t), path, []string{testCoverID})
		if count <= MaxCopyCoverWorks {
			if err != nil || len(got) != count {
				t.Fatal(count, len(got), err)
			}
		} else if got != nil || !errors.Is(err, ErrCopyCoverWorkLimit) {
			t.Fatal(len(got), err)
		}
		copyCoverWork(t, s, "covers/"+testCoverID+".png")
	}
}

func TestCopyCoverWorksUnavailableAndEmpty(t *testing.T) {
	s, path := detailFixture(t, 1, "題")
	got, err := DatabaseCopyCoverWorks(bounded(t), path, nil)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	for _, ids := range [][]string{{"bad"}, {testCoverID, testCoverID}, {strings.Repeat("b", 32)}, make([]string, 10001)} {
		got, err := DatabaseCopyCoverWorks(bounded(t), path, ids)
		if got != nil || !errors.Is(err, ErrDatabaseCopyCoverWorks) {
			t.Fatal(len(got), err)
		}
	}
	ids := make([]string, 10000)
	for i := range ids {
		ids[i] = fmt.Sprintf("%032x", i)
	}
	// Actual pinned SQLite must accept all format-sized bind parameters. Missing
	// inventory is the precise expected rejection, not a prepare/variable error.
	if got, err := DatabaseCopyCoverWorks(bounded(t), path, ids); got != nil || err == nil || err.Error() != errors.Join(ErrDatabaseCopyCoverWorks, ErrDatabaseCopyCoverWorks).Error() {
		t.Fatal(len(got), err)
	}
	if _, err := s.db.Exec("UPDATE works SET title=CAST(? AS TEXT)", []byte{0xff}); err != nil {
		t.Fatal(err)
	}
	if got, err := DatabaseCopyCoverWorks(bounded(t), path, []string{testCoverID}); got != nil || !errors.Is(err, ErrDatabaseCopyCoverWorks) {
		t.Fatal(got, err)
	}
	ctx, cancel := context.WithCancel(bounded(t))
	cancel()
	if got, err := DatabaseCopyCoverWorks(ctx, path, nil); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatal(got, err)
	}
	missing := filepath.Join(t.TempDir(), "missing.sqlite")
	if got, err := DatabaseCopyCoverWorks(bounded(t), missing, nil); got != nil || err == nil {
		t.Fatal(got, err)
	}
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("reader created missing copy", err)
	}
}

func TestCopyCoverWorksReadAndCleanupFaults(t *testing.T) {
	_, path := detailFixture(t, 1, "題")
	for _, mode := range []string{"rows", "rollback", "connection", "cancel-close", "read"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(bounded(t))
			defer cancel()
			name := fmt.Sprintf("cover-work-detail-%d", faultDriverID.Add(1))
			cause := syntheticCleanup
			switch mode {
			case "connection":
				sql.Register(name, validationCloseDriver{})
				cause = validationCloseFault
			case "cancel-close":
				sql.Register(name, validationCloseDriver{cancel: cancel})
				cause = context.Canceled
			case "read":
				sql.Register(name, detailReadFaultDriver{})
			default:
				sql.Register(name, cleanupDriver{rows: mode == "rows", commit: mode == "rows"})
			}
			db, err := sql.Open(name, path)
			if err != nil {
				t.Fatal(err)
			}
			db.SetMaxOpenConns(1)
			got, err := readDatabaseCopyCoverWorks(ctx, db, []string{testCoverID})
			if got != nil || !errors.Is(err, cause) || errors.Is(err, ErrDatabaseCopyRelease) != (mode != "cancel-close") {
				t.Fatal(got, err)
			}
			if db.Ping() == nil {
				t.Fatal("reader did not close its database")
			}
		})
	}
}

type detailReadFaultDriver struct{}
type detailReadFaultConn struct{ driver.Conn }
type detailReadFaultRows struct {
	driver.Rows
	read bool
}

func (detailReadFaultDriver) Open(path string) (driver.Conn, error) {
	c, err := (&sqlite.Driver{}).Open(path)
	return detailReadFaultConn{c}, err
}
func (c detailReadFaultConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	r, err := c.Conn.(driver.QueryerContext).QueryContext(ctx, query, args)
	if err != nil {
		return nil, err
	}
	return &detailReadFaultRows{Rows: r}, nil
}
func (r *detailReadFaultRows) Next(out []driver.Value) error {
	if r.read {
		return syntheticCleanup
	}
	r.read = true
	return r.Rows.Next(out)
}
