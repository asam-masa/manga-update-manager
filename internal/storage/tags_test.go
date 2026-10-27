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

func withTags(w manga.Work, existing []manga.Tag, names ...string) manga.EditWorkInput {
	input := textOf(w)
	input.Tags = &manga.TagEdit{Existing: existing, NewNames: names}
	return input
}

func TestTagEditLifecycleAndCompatibility(t *testing.T) {
	ctx := bounded(t)
	s, path := managedRecordStore(t)
	w := coverReferenceWork(t, s)
	date := manga.ScheduledDate("2028-02-29")
	w, err := s.EditWork(ctx, w.ID, dateEdit(w, date), w.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.ListTags(ctx); err != nil || got == nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	if got, err := s.WorkTags(ctx, w.ID); err != nil || got == nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	if _, err := s.WorkTags(ctx, 999); !errors.Is(err, ErrWorkNotFound) {
		t.Fatal(err)
	}
	want := w
	want.UpdatedAt = w.UpdatedAt.Add(time.Hour)
	w, err = s.EditWork(ctx, w.ID, withTags(w, nil, " 異世界 ", "連載中", "異世界", "ABC", "abc"), want.UpdatedAt)
	if err != nil || !reflect.DeepEqual(w, want) {
		t.Fatal(w, want, err)
	}
	tags, err := s.WorkTags(ctx, w.ID)
	if err != nil || len(tags) != 4 || tags[0].Name != "異世界" || tags[1].Name != "連載中" {
		t.Fatal(tags, err)
	}
	// New creation intents may name saved tags; same-name duplicates and selected
	// IDs collapse into the same association without UPDATE or empty Commit.
	if _, err := s.db.Exec("CREATE TRIGGER reject_noop BEFORE UPDATE ON works BEGIN SELECT RAISE(ABORT,'unexpected UPDATE'); END"); err != nil {
		t.Fatal(err)
	}
	got, err := s.EditWork(ctx, w.ID, withTags(w, []manga.Tag{tags[3], tags[0]}, "異世界", "連載中", "ABC", "abc"), w.UpdatedAt.Add(time.Hour))
	if err != nil || !reflect.DeepEqual(got, w) {
		t.Fatal(got, err)
	}
	if _, err := s.db.Exec("DROP TRIGGER reject_noop"); err != nil {
		t.Fatal(err)
	}
	// The old input/API has no tag field. Text-only edits preserve all associations.
	input := textOf(w)
	input.Notes = "text-only"
	w, err = s.EditWork(ctx, w.ID, input, w.UpdatedAt.Add(time.Hour))
	if err != nil || w.ScheduledDate != date {
		t.Fatal(w, err)
	}
	if got, err := s.WorkTags(ctx, w.ID); err != nil || !reflect.DeepEqual(got, tags) {
		t.Fatal(got, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, err := s.WorkTags(ctx, w.ID); err != nil || !reflect.DeepEqual(got, tags) {
		t.Fatal(got, err)
	}
}

func TestTagsSharedReuseAndClear(t *testing.T) {
	ctx := bounded(t)
	s, _ := managedRecordStore(t)
	w := coverReferenceWork(t, s)
	w, err := s.EditWork(ctx, w.ID, withTags(w, nil, "共通", "未使用候補"), w.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	all, err := s.ListTags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := manga.NewWork(manga.NewWorkInput{URL: "https://example.com/other-tags", Title: "Other"}, w.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateWork(ctx, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.EditWork(ctx, other.ID, withTags(other, nil, "共通"), other.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ListTags(ctx); err != nil || !reflect.DeepEqual(got, all) {
		t.Fatal(got, err)
	}
	before := w
	w, err = s.EditWork(ctx, w.ID, withTags(w, nil), w.CreatedAt.Add(-time.Hour))
	if err != nil || !reflect.DeepEqual(w, before) {
		t.Fatal(w, err)
	}
	if got, err := s.WorkTags(ctx, w.ID); err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	if got, err := s.ListTags(ctx); err != nil || !reflect.DeepEqual(got, all) {
		t.Fatal(got, err)
	}
	if _, err := s.DeleteWork(ctx, other.ID, deleteInput(other)); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ListTags(ctx); err != nil || !reflect.DeepEqual(got, all) {
		t.Fatal(got, err)
	}
	var links int
	if err := s.db.QueryRow("SELECT count(*) FROM work_tags").Scan(&links); err != nil || links != 0 {
		t.Fatal(links, err)
	}
}

func TestTagChangesRefuseWithoutPartialSave(t *testing.T) {
	for _, change := range []string{"rename", "delete", "work-update-failure", "attach-failure", "ignored-create", "ignored-attach", "ignored-detach"} {
		t.Run(change, func(t *testing.T) {
			ctx := bounded(t)
			s, _ := managedRecordStore(t)
			w := coverReferenceWork(t, s)
			w, err := s.EditWork(ctx, w.ID, withTags(w, nil, "既存"), w.UpdatedAt)
			if err != nil {
				t.Fatal(err)
			}
			tags, err := s.WorkTags(ctx, w.ID)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "rename":
				_, err = s.db.Exec("UPDATE tags SET name='改名' WHERE id=?", tags[0].ID)
			case "delete":
				_, err = s.db.Exec("DELETE FROM tags WHERE id=?", tags[0].ID)
			case "work-update-failure":
				_, err = s.db.Exec("CREATE TRIGGER reject_work BEFORE UPDATE ON works BEGIN SELECT RAISE(ABORT,'test'); END")
			case "attach-failure":
				_, err = s.db.Exec("CREATE TRIGGER reject_link BEFORE INSERT ON work_tags BEGIN SELECT RAISE(ABORT,'test'); END")
			case "ignored-create":
				_, err = s.db.Exec("CREATE TRIGGER ignore_tag BEFORE INSERT ON tags BEGIN SELECT RAISE(IGNORE); END")
			case "ignored-attach":
				_, err = s.db.Exec("CREATE TRIGGER ignore_link BEFORE INSERT ON work_tags BEGIN SELECT RAISE(IGNORE); END")
			case "ignored-detach":
				_, err = s.db.Exec("CREATE TRIGGER ignore_unlink BEFORE DELETE ON work_tags BEGIN SELECT RAISE(IGNORE); END")
			}
			if err != nil {
				t.Fatal(err)
			}
			before, err := s.ListTags(ctx)
			if err != nil {
				t.Fatal(err)
			}
			links, err := s.WorkTags(ctx, w.ID)
			if err != nil {
				t.Fatal(err)
			}
			input := withTags(w, tags, "新規1", "新規2")
			if change == "ignored-detach" {
				input.Tags.Existing = nil
			}
			input.Title = "未確定の変更"
			got, err := s.EditWork(ctx, w.ID, input, w.UpdatedAt.Add(time.Hour))
			if err == nil || got.ID != 0 {
				t.Fatal(got, err)
			}
			if (change == "rename" || change == "delete") && !errors.Is(err, ErrWorkTagsChanged) {
				t.Fatal(err)
			}
			if got, err := s.WorkByID(ctx, w.ID); err != nil || !reflect.DeepEqual(got, w) {
				t.Fatal(got, err)
			}
			if got, err := s.ListTags(ctx); err != nil || !reflect.DeepEqual(got, before) {
				t.Fatal(got, err)
			}
			if got, err := s.WorkTags(ctx, w.ID); err != nil || !reflect.DeepEqual(got, links) {
				t.Fatal(got, err)
			}
		})
	}
}

func TestTagDeleteSnapshotAndReadRefusals(t *testing.T) {
	ctx := bounded(t)
	s, _ := managedRecordStore(t)
	w := coverReferenceWork(t, s)
	w, err := s.EditWork(ctx, w.ID, withTags(w, nil, "タグ"), w.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	tags, err := s.WorkTags(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	expected := deleteInput(w)
	empty := []manga.Tag{}
	expected.Tags = &empty
	if _, err := s.DeleteWork(ctx, w.ID, expected); !errors.Is(err, ErrWorkDeleteChanged) {
		t.Fatal(err)
	}
	expected.Tags = &tags
	if _, err := s.db.Exec("UPDATE tags SET name='改名' WHERE id=?", tags[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteWork(ctx, w.ID, expected); !errors.Is(err, ErrWorkDeleteChanged) {
		t.Fatal(err)
	}
	current, err := s.WorkTags(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	expected.Tags = &current
	if _, err := s.DeleteWork(ctx, w.ID, expected); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ListTags(ctx); err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.ListTags(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	s.admission.stop()
	if got, err := s.ListTags(ctx); !errors.Is(err, ErrDBStopped) || got != nil {
		t.Fatal(got, err)
	}
	if got, err := s.WorkTags(ctx, w.ID); !errors.Is(err, ErrDBStopped) || got != nil {
		t.Fatal(got, err)
	}
}

func TestTagEditCommitUnknownStopsAndPersistsAtomically(t *testing.T) {
	ctx := bounded(t)
	original, path := managedRecordStore(t)
	w := coverReferenceWork(t, original)
	w, err := original.EditWork(ctx, w.ID, withTags(w, nil, "既存"), w.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	existing, err := original.WorkTags(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := original.Close(); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("tag-commit-fault-%d", faultDriverID.Add(1))
	sql.Register(name, cleanupDriver{commit: true})
	db, err := sql.Open(name, path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	defer s.Close()
	if got, err := s.EditWork(ctx, w.ID, withTags(w, nil, "既存"), w.UpdatedAt.Add(time.Hour)); err != nil || !reflect.DeepEqual(got, w) {
		t.Fatal("no-op committed", got, err)
	}
	input := withTags(w, existing, "新規1", "新規2")
	input.Title = "確定した変更"
	got, err := s.EditWork(ctx, w.ID, input, w.UpdatedAt.Add(time.Hour))
	if got.ID != 0 || !errors.Is(err, ErrWorkEditOutcomeUnknown) || !errors.Is(err, ErrDBStopped) {
		t.Fatal(got, err)
	}
	if _, err := s.ListTags(ctx); !errors.Is(err, ErrDBStopped) {
		t.Fatal(err)
	}
	reader, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	after, err := reader.WorkByID(ctx, w.ID)
	if err != nil || after.Title != input.Title {
		t.Fatal(after, err)
	}
	tags, err := reader.WorkTags(ctx, w.ID)
	if err != nil || len(tags) != 3 {
		t.Fatal(tags, err)
	}
}

func TestTagReadCorruptionAndCleanupDoNotReturnPartialData(t *testing.T) {
	ctx := bounded(t)
	original, path := managedRecordStore(t)
	w := coverReferenceWork(t, original)
	w, err := original.EditWork(ctx, w.ID, withTags(w, nil, "正常", "破損対象"), w.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := original.db.Exec("UPDATE tags SET name=' 前後空白 ' WHERE name='破損対象'"); err != nil {
		t.Fatal(err)
	}
	if got, err := original.ListTags(ctx); err == nil || got != nil {
		t.Fatal(got, err)
	}
	if got, err := original.WorkTags(ctx, w.ID); err == nil || got != nil {
		t.Fatal(got, err)
	}
	var raw string
	if err := original.db.QueryRow("SELECT name FROM tags WHERE id=2").Scan(&raw); err != nil || raw != " 前後空白 " {
		t.Fatal("repaired stored name", raw, err)
	}
	// Restore only the synthetic fixture, then inject a Rows.Close failure.
	if _, err := original.db.Exec("UPDATE tags SET name='破損対象' WHERE id=2"); err != nil {
		t.Fatal(err)
	}
	if err := original.Close(); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("tag-rows-fault-%d", faultDriverID.Add(1))
	sql.Register(name, cleanupDriver{rows: true})
	db, err := sql.Open(name, path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	defer s.Close()
	if got, err := s.ListTags(ctx); got != nil || !errors.Is(err, ErrDBStopped) || !errors.Is(err, syntheticCleanup) {
		t.Fatal(got, err)
	}
	if _, err := s.WorkTags(ctx, w.ID); !errors.Is(err, ErrDBStopped) {
		t.Fatal(err)
	}
}

func TestTagRollbackCleanupFailureStopsWithoutSaving(t *testing.T) {
	ctx := bounded(t)
	original, path := managedRecordStore(t)
	w := coverReferenceWork(t, original)
	if _, err := original.db.Exec("CREATE TRIGGER reject_work BEFORE UPDATE ON works BEGIN SELECT RAISE(ABORT,'test'); END"); err != nil {
		t.Fatal(err)
	}
	if err := original.Close(); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("tag-rollback-fault-%d", faultDriverID.Add(1))
	sql.Register(name, cleanupDriver{commit: false})
	db, err := sql.Open(name, path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	defer s.Close()
	got, err := s.EditWork(ctx, w.ID, withTags(w, nil, "未確定"), w.UpdatedAt)
	if got.ID != 0 || !errors.Is(err, syntheticCleanup) || !errors.Is(err, ErrDBStopped) || errors.Is(err, ErrWorkEditOutcomeUnknown) {
		t.Fatal(got, err)
	}
	reader, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if got, err := reader.WorkByID(ctx, w.ID); err != nil || !reflect.DeepEqual(got, w) {
		t.Fatal(got, err)
	}
	if got, err := reader.ListTags(ctx); err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
}
