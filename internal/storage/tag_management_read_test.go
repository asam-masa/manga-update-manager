package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
	"modernc.org/sqlite"
)

func managementReadFixture(t *testing.T) (*Store, string, manga.Work, manga.Work) {
	t.Helper()
	s, path := managedRecordStore(t)
	w := coverReferenceWork(t, s)
	if _, err := s.EditWork(bounded(t), w.ID, withTags(w, nil, "共有", "未使用"), w.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	other, err := manga.NewWork(manga.NewWorkInput{URL: "https://example.com/second", Title: "同じタイトル"}, w.CreatedAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	other, err = s.CreateWork(bounded(t), other)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.EditWork(bounded(t), other.ID, withTags(other, nil, "共有"), other.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EditWork(bounded(t), w.ID, withTags(w, nil, "共有"), w.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	return s, path, w, other
}

func TestTagManagementReadsAndReopen(t *testing.T) {
	ctx := bounded(t)
	empty, _ := managedRecordStore(t)
	if got, err := empty.ListTagUsage(ctx); err != nil || got == nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	s, path, w, other := managementReadFixture(t)
	before, err := s.ListWorks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var changes int64
	if err := s.db.QueryRow("SELECT total_changes()").Scan(&changes); err != nil {
		t.Fatal(err)
	}
	want := []TagUsage{{Tag: manga.Tag{ID: 1, Name: "共有"}, WorkCount: 2}, {Tag: manga.Tag{ID: 2, Name: "未使用"}}}
	if got, err := s.ListTagUsage(ctx); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, err)
	}
	preview, err := s.PreviewTagDelete(ctx, 1)
	if err != nil || preview.Tag != want[0].Tag || !reflect.DeepEqual(preview.Works, []TagWork{{w.ID, w.CreatedAt, w.Title}, {other.ID, other.CreatedAt, other.Title}}) {
		t.Fatal(preview, err)
	}
	if got, err := s.PreviewTagDelete(ctx, 2); err != nil || got.Tag != want[1].Tag || got.Works == nil || len(got.Works) != 0 {
		t.Fatal(got, err)
	}
	if got, err := s.PreviewTagDelete(ctx, 999); !errors.Is(err, ErrTagNotFound) || !reflect.DeepEqual(got, TagDeletePreview{}) {
		t.Fatal(got, err)
	}
	var afterChanges int64
	if err := s.db.QueryRow("SELECT total_changes()").Scan(&afterChanges); err != nil || afterChanges != changes {
		t.Fatal("read mutated database", changes, afterChanges, err)
	}
	if got, err := s.ListWorks(ctx); err != nil || !reflect.DeepEqual(got, before) {
		t.Fatal(got, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got, err := reopened.ListTagUsage(ctx); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, err)
	}
	if got, err := reopened.PreviewTagDelete(ctx, 1); err != nil || !reflect.DeepEqual(got, preview) {
		t.Fatal(got, err)
	}
}

func TestTagManagementReadRefusals(t *testing.T) {
	for _, query := range []string{
		"UPDATE tags SET name=' 不正 ' WHERE id=2",
		"UPDATE works SET title='' WHERE id=2",
		"UPDATE works SET created_at='invalid' WHERE id=2",
		"UPDATE works SET created_at='0001-01-01T00:00:00Z' WHERE id=2",
		"PRAGMA foreign_keys=OFF; DELETE FROM works WHERE id=2",
	} {
		t.Run(query, func(t *testing.T) {
			s, _, _, _ := managementReadFixture(t)
			// Only this disposable fixture bypasses CHECKs to model stored corruption.
			if _, err := s.db.Exec("PRAGMA ignore_check_constraints=ON"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.Exec(query); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(query, "UPDATE tags") || strings.Contains(query, "DELETE FROM") {
				if got, err := s.ListTagUsage(bounded(t)); err == nil || got != nil {
					t.Fatal(got, err)
				}
			}
			id := int64(1)
			if strings.Contains(query, "UPDATE tags") {
				id = 2
			}
			if got, err := s.PreviewTagDelete(bounded(t), id); err == nil || !reflect.DeepEqual(got, TagDeletePreview{}) {
				t.Fatal(got, err)
			}
		})
	}
	for _, preview := range []bool{false, true} {
		t.Run(fmt.Sprint(preview), func(t *testing.T) {
			s, path, _, _ := managementReadFixture(t)
			read := func(ctx context.Context, store *Store) error {
				if preview {
					got, err := store.PreviewTagDelete(ctx, 1)
					if !reflect.DeepEqual(got, TagDeletePreview{}) {
						t.Fatal(got)
					}
					return err
				}
				got, err := store.ListTagUsage(ctx)
				if got != nil {
					t.Fatal(got)
				}
				return err
			}
			cancelled, cancel := context.WithCancel(bounded(t))
			cancel()
			if err := read(cancelled, s); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			if err := read(bounded(t), s); !errors.Is(err, ErrDBStopped) {
				t.Fatal(err)
			}
			name := fmt.Sprintf("management-rows-fault-%d", faultDriverID.Add(1))
			sql.Register(name, cleanupDriver{rows: true})
			db, err := sql.Open(name, path)
			if err != nil {
				t.Fatal(err)
			}
			db.SetMaxOpenConns(1)
			faulty := &Store{db: db}
			defer faulty.Close()
			if err := read(bounded(t), faulty); !errors.Is(err, ErrDBStopped) || !errors.Is(err, syntheticCleanup) {
				t.Fatal(err)
			}
			if err := read(bounded(t), faulty); !errors.Is(err, ErrDBStopped) {
				t.Fatal(err)
			}
		})
	}
}

func TestTagDeletePreviewDistinguishesReusedWorkID(t *testing.T) {
	s, _, _, old := managementReadFixture(t)
	before, err := s.PreviewTagDelete(bounded(t), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteWork(bounded(t), old.ID, deleteInput(old)); err != nil {
		t.Fatal(err)
	}
	candidate, err := manga.NewWork(manga.NewWorkInput{URL: old.URL, Title: old.Title}, old.CreatedAt.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := s.CreateWork(bounded(t), candidate)
	if err != nil || replacement.ID != old.ID {
		t.Fatal(replacement, err)
	}
	if _, err := s.EditWork(bounded(t), replacement.ID, withTags(replacement, nil, "共有"), replacement.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	after, err := s.PreviewTagDelete(bounded(t), 1)
	if err != nil || len(after.Works) != 2 || after.Works[1].ID != before.Works[1].ID || after.Works[1].Title != before.Works[1].Title || after.Works[1].CreatedAt.Equal(before.Works[1].CreatedAt) {
		t.Fatal(before, after, err)
	}
}

func TestTagDeletePreviewUsesOneSnapshot(t *testing.T) {
	s, path, _, other := managementReadFixture(t)
	if _, err := s.db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		t.Fatal(err)
	}
	before, err := s.PreviewTagDelete(bounded(t), 1)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	name := fmt.Sprintf("management-snapshot-%d", faultDriverID.Add(1))
	sql.Register(name, previewStepDriver{afterFirst: func() {
		called = true
		// External writer commits after the first result row, before the second.
		if _, err := s.db.Exec("UPDATE tags SET name='変更後' WHERE id=1; DELETE FROM work_tags WHERE tag_id=1 AND work_id=?", other.ID); err != nil {
			t.Fatal(err)
		}
	}})
	db, err := sql.Open(name, path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	reader := &Store{db: db}
	defer reader.Close()
	if got, err := reader.PreviewTagDelete(bounded(t), 1); err != nil || !called || !reflect.DeepEqual(got, before) {
		t.Fatal(got, called, err)
	}
	if got, err := s.PreviewTagDelete(bounded(t), 1); err != nil || got.Tag.Name != "変更後" || len(got.Works) != 1 {
		t.Fatal(got, err)
	}
}

// A test-only seam around real SQLite Rows, with no production hook or timer.
type previewStepDriver struct{ afterFirst func() }

func (d previewStepDriver) Open(name string) (driver.Conn, error) {
	c, err := (&sqlite.Driver{}).Open(name)
	return previewStepConn{Conn: c, afterFirst: d.afterFirst}, err
}

type previewStepConn struct {
	driver.Conn
	afterFirst func()
}

func (c previewStepConn) Prepare(query string) (driver.Stmt, error) {
	stmt, err := c.Conn.Prepare(query)
	return previewStepStmt{Stmt: stmt, afterFirst: c.afterFirst}, err
}

type previewStepStmt struct {
	driver.Stmt
	afterFirst func()
}

func (s previewStepStmt) Query(args []driver.Value) (driver.Rows, error) {
	rows, err := s.Stmt.Query(args)
	return &previewStepRows{Rows: rows, afterFirst: s.afterFirst}, err
}

type previewStepRows struct {
	driver.Rows
	afterFirst func()
	read       bool
}

func (r *previewStepRows) Next(values []driver.Value) error {
	if r.read && r.afterFirst != nil {
		r.afterFirst()
		r.afterFirst = nil
	}
	err := r.Rows.Next(values)
	r.read = true
	return err
}
