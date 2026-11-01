package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
)

func TestLibrarySnapshotEmptyFullAndReopen(t *testing.T) {
	empty, _ := managedRecordStore(t)
	got, err := empty.LoadLibrarySnapshot(bounded(t))
	if err != nil || got.Works == nil || got.Tags == nil || got.Links == nil || len(got.Works)+len(got.Tags)+len(got.Links) != 0 {
		t.Fatal(got, err)
	}
	s, path, w, other := managementReadFixture(t)
	plain, err := manga.NewWork(manga.NewWorkInput{URL: "https://example.com/plain", Title: "タグなし", SiteName: "合成", Notes: "メモ"}, w.CreatedAt.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	plain.ScheduledDate = "2028-02-29"
	plain, err = s.CreateWork(bounded(t), plain)
	if err != nil {
		t.Fatal(err)
	}
	at := plain.CreatedAt.Add(123456789 * time.Nanosecond)
	if err := s.RecordLastAccess(bounded(t), plain.ID, at); err != nil {
		t.Fatal(err)
	}
	before, err := s.ListWorks(bounded(t))
	if err != nil {
		t.Fatal(err)
	}
	var changes int64
	if err := s.db.QueryRow("SELECT total_changes()").Scan(&changes); err != nil {
		t.Fatal(err)
	}
	want := LibrarySnapshot{Works: before, Tags: []manga.Tag{{ID: 1, Name: "共有"}, {ID: 2, Name: "未使用"}}, Links: []WorkTagLink{{w.ID, 1}, {other.ID, 1}}}
	got, err = s.LoadLibrarySnapshot(bounded(t))
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, err)
	}
	var after int64
	if err := s.db.QueryRow("SELECT total_changes()").Scan(&after); err != nil || changes != after {
		t.Fatal("read changed data", changes, after, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(bounded(t), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got, err := reopened.LoadLibrarySnapshot(bounded(t)); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, err)
	}
}

func TestLibrarySnapshotRejectsPartialOrInvalidData(t *testing.T) {
	for _, query := range []string{
		"UPDATE works SET title='' WHERE id=2",
		"UPDATE works SET created_at='invalid' WHERE id=2",
		"UPDATE works SET updated_at='0001-01-01T00:00:00Z' WHERE id=2",
		"UPDATE works SET last_accessed_at='invalid' WHERE id=2",
		"UPDATE works SET scheduled_date='2028-02-30' WHERE id=2",
		"UPDATE works SET url='file:///private' WHERE id=2",
		"UPDATE works SET thumbnail_path='../private' WHERE id=2",
		"UPDATE tags SET name=' 不正 ' WHERE id=2",
		"PRAGMA foreign_keys=OFF; DELETE FROM works WHERE id=2",
		"PRAGMA foreign_keys=OFF; DELETE FROM tags WHERE id=1",
		"DROP TABLE work_tags",
		// Disposable fixture only: remove schema constraints to exercise read guards.
		"ALTER TABLE work_tags RENAME TO old_links; CREATE TABLE work_tags AS SELECT * FROM old_links; INSERT INTO work_tags SELECT * FROM old_links",
		"PRAGMA foreign_keys=OFF; ALTER TABLE tags RENAME TO old_tags; CREATE TABLE tags AS SELECT * FROM old_tags; INSERT INTO tags SELECT * FROM old_tags",
		"PRAGMA foreign_keys=OFF; ALTER TABLE tags RENAME TO old_tags; CREATE TABLE tags AS SELECT * FROM old_tags; UPDATE tags SET name='共有' WHERE id=2",
	} {
		t.Run(query, func(t *testing.T) {
			s, _, _, _ := managementReadFixture(t)
			if _, err := s.db.Exec("PRAGMA ignore_check_constraints=ON"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.Exec(query); err != nil {
				t.Fatal(err)
			}
			got, err := s.LoadLibrarySnapshot(bounded(t))
			if err == nil || !reflect.DeepEqual(got, LibrarySnapshot{}) {
				t.Fatal("partial result exposed", got, err)
			}
		})
	}
}

func TestLibrarySnapshotKeepsInt64IDs(t *testing.T) {
	s, _, _, other := managementReadFixture(t)
	const maximum int64 = 9223372036854775807
	// Only the isolated fixture bypasses FK checks to assign exact boundary IDs.
	if _, err := s.db.Exec("PRAGMA foreign_keys=OFF; UPDATE works SET id=? WHERE id=?; UPDATE work_tags SET work_id=? WHERE work_id=?; UPDATE tags SET id=? WHERE id=2", maximum, other.ID, maximum, other.ID, maximum); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadLibrarySnapshot(bounded(t))
	if err != nil || got.Works[1].ID != maximum || got.Tags[1].ID != maximum || got.Links[1].WorkID != maximum || !got.Works[1].CreatedAt.Equal(other.CreatedAt) {
		t.Fatal(got, err)
	}
}

func TestLibrarySnapshotOneReadViewAcrossExternalChanges(t *testing.T) {
	s, path, _, other := managementReadFixture(t)
	if _, err := s.db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		t.Fatal(err)
	}
	before, err := s.LoadLibrarySnapshot(bounded(t))
	if err != nil {
		t.Fatal(err)
	}
	called := false
	name := fmt.Sprintf("library-snapshot-%d", faultDriverID.Add(1))
	sql.Register(name, previewStepDriver{afterFirst: func() {
		called = true
		// The writer commits while the reader is still walking the first SELECT.
		if _, err := s.db.Exec("UPDATE works SET title='変更後' WHERE id=?; UPDATE tags SET name='改名後' WHERE id=1; DELETE FROM work_tags WHERE work_id=?", other.ID, other.ID); err != nil {
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
	got, err := reader.LoadLibrarySnapshot(bounded(t))
	if err != nil || !called || !reflect.DeepEqual(got, before) {
		t.Fatal("mixed read views", got, called, err)
	}
	after, err := s.LoadLibrarySnapshot(bounded(t))
	if err != nil || after.Works[1].Title != "変更後" || after.Tags[0].Name != "改名後" || len(after.Links) != 1 {
		t.Fatal(after, err)
	}
}

func TestLibrarySnapshotCancellationAndCleanupFailures(t *testing.T) {
	s, path, _, _ := managementReadFixture(t)
	cancelled, cancel := context.WithCancel(bounded(t))
	cancel()
	if got, err := s.LoadLibrarySnapshot(cancelled); !errors.Is(err, context.Canceled) || !reflect.DeepEqual(got, LibrarySnapshot{}) {
		t.Fatal(got, err)
	}
	if _, err := s.LoadLibrarySnapshot(bounded(t)); err != nil {
		t.Fatal("cancel before admission should not stop DB", err)
	}
	for _, rowsFault := range []bool{false, true} {
		t.Run(fmt.Sprint(rowsFault), func(t *testing.T) {
			name := fmt.Sprintf("library-cleanup-%d", faultDriverID.Add(1))
			// commit=true leaves Rollback healthy, isolating the Rows fault.
			sql.Register(name, cleanupDriver{rows: rowsFault, commit: rowsFault})
			db, err := sql.Open(name, path)
			if err != nil {
				t.Fatal(err)
			}
			db.SetMaxOpenConns(1)
			faulty := &Store{db: db}
			defer faulty.Close()
			if got, err := faulty.LoadLibrarySnapshot(bounded(t)); !errors.Is(err, ErrDBStopped) || !errors.Is(err, syntheticCleanup) || !reflect.DeepEqual(got, LibrarySnapshot{}) {
				t.Fatal(got, err)
			}
			if _, err := faulty.LoadLibrarySnapshot(bounded(t)); !errors.Is(err, ErrDBStopped) {
				t.Fatal(err)
			}
		})
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if got, err := s.LoadLibrarySnapshot(bounded(t)); !errors.Is(err, ErrDBStopped) || !reflect.DeepEqual(got, LibrarySnapshot{}) {
		t.Fatal(got, err)
	}
}
