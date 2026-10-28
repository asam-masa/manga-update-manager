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

func tagPreview(t *testing.T, s *Store, id int64) TagDeletePreview {
	t.Helper()
	got, err := s.PreviewTagDelete(bounded(t), id)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestTagManagementChangesPreserveWorksAndReopen(t *testing.T) {
	s, path, w, _ := managementReadFixture(t)
	date := manga.ScheduledDate("2028-02-29")
	if _, err := s.EditWork(bounded(t), w.ID, dateEdit(w, date), w.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EditWork(bounded(t), w.ID, withTags(w, []manga.Tag{{ID: 1, Name: "共有"}}, "他タグ"), w.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	readyCover(t, s, testCoverID)
	if _, _, err := s.SetWorkCover(bounded(t), w.ID, testCoverID, w.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	before, err := s.ListWorks(bounded(t))
	if err != nil {
		t.Fatal(err)
	}
	covers, err := s.ListManagedCovers(bounded(t))
	if err != nil {
		t.Fatal(err)
	}
	preview := tagPreview(t, s, 1)
	renamed, err := s.RenameTag(bounded(t), preview.Tag, " 改名 ")
	if err != nil || renamed != (manga.Tag{ID: 1, Name: "改名"}) {
		t.Fatal(renamed, err)
	}
	for _, work := range before {
		want := []manga.Tag{renamed}
		if work.ID == w.ID {
			want = append(want, manga.Tag{ID: 3, Name: "他タグ"})
		}
		if got, err := s.WorkTags(bounded(t), work.ID); err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal(got, err)
		}
	}
	confirmed := tagPreview(t, s, renamed.ID)
	// Titles and ordering are not membership/revision tokens.
	confirmed.Works[0], confirmed.Works[1] = confirmed.Works[1], confirmed.Works[0]
	confirmed.Works[0].Title = "確認時の表示名"
	if got, err := s.DeleteTag(bounded(t), confirmed); err != nil || got != renamed {
		t.Fatal(got, err)
	}
	if got, err := s.ListTagUsage(bounded(t)); err != nil || !reflect.DeepEqual(got, []TagUsage{{Tag: manga.Tag{ID: 2, Name: "未使用"}}, {Tag: manga.Tag{ID: 3, Name: "他タグ"}, WorkCount: 1}}) {
		t.Fatal(got, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(bounded(t), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got, err := reopened.ListWorks(bounded(t)); err != nil || !reflect.DeepEqual(got, before) {
		t.Fatal(got, err)
	}
	if got, err := reopened.ListManagedCovers(bounded(t)); err != nil || !reflect.DeepEqual(got, covers) {
		t.Fatal(got, err)
	}
	for _, work := range before {
		want := []manga.Tag{}
		if work.ID == w.ID {
			want = append(want, manga.Tag{ID: 3, Name: "他タグ"})
		}
		if got, err := reopened.WorkTags(bounded(t), work.ID); err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal(got, err)
		}
	}
	unused := tagPreview(t, reopened, 2)
	if _, err := reopened.DeleteTag(bounded(t), unused); err != nil {
		t.Fatal(err)
	}
}

func TestTagRenameConfirmationDuplicatesAndNoop(t *testing.T) {
	s, _, w, _ := managementReadFixture(t)
	preview := tagPreview(t, s, 1)
	if _, err := s.RenameTag(bounded(t), preview.Tag, " 未使用 "); !errors.Is(err, ErrTagDuplicateName) {
		t.Fatal(err)
	}
	if _, err := s.RenameTag(bounded(t), preview.Tag, "\n"); !errors.Is(err, manga.ErrTagName) {
		t.Fatal(err)
	}
	if _, err := s.EditWork(bounded(t), w.ID, withTags(w, nil), w.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	current, err := s.RenameTag(bounded(t), preview.Tag, "変更後")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RenameTag(bounded(t), preview.Tag, "上書き"); !errors.Is(err, ErrTagChanged) {
		t.Fatal(err)
	}
	if _, err := s.DeleteTag(bounded(t), preview); !errors.Is(err, ErrTagChanged) {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("CREATE TRIGGER reject_tag_update BEFORE UPDATE ON tags BEGIN SELECT RAISE(ABORT,'unexpected update'); END"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.RenameTag(bounded(t), current, " 変更後 "); err != nil || got != current {
		t.Fatal(got, err)
	}
	if _, err := s.DeleteTag(bounded(t), tagPreview(t, s, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RenameTag(bounded(t), current, "再作成しない"); !errors.Is(err, ErrTagNotFound) {
		t.Fatal(err)
	}
}

func TestTagDeleteRejectsChangedMembers(t *testing.T) {
	for _, change := range []string{"same-count-swap", "reused-id", "created-time", "duplicate", "nil"} {
		t.Run(change, func(t *testing.T) {
			s, _, _, old := managementReadFixture(t)
			expected := tagPreview(t, s, 1)
			wantErr := ErrTagChanged
			switch change {
			case "same-count-swap", "reused-id":
				if change == "reused-id" {
					if _, err := s.DeleteWork(bounded(t), old.ID, deleteInput(old)); err != nil {
						t.Fatal(err)
					}
				} else if _, err := s.EditWork(bounded(t), old.ID, withTags(old, nil), old.UpdatedAt); err != nil {
					t.Fatal(err)
				}
				candidate, err := manga.NewWork(manga.NewWorkInput{URL: "https://example.com/replacement", Title: old.Title}, old.CreatedAt.Add(time.Hour))
				if err != nil {
					t.Fatal(err)
				}
				work, err := s.CreateWork(bounded(t), candidate)
				if err != nil || (change == "reused-id" && work.ID != old.ID) {
					t.Fatal(work, err)
				}
				if _, err := s.EditWork(bounded(t), work.ID, withTags(work, nil, "共有"), work.UpdatedAt); err != nil {
					t.Fatal(err)
				}
			case "created-time":
				expected.Works[0].CreatedAt = expected.Works[0].CreatedAt.Add(time.Second)
			case "duplicate":
				expected.Works[1] = expected.Works[0]
				wantErr = ErrTagConfirmation
			case "nil":
				expected.Works = nil
				wantErr = ErrTagConfirmation
			}
			before := tagPreview(t, s, 1)
			if got, err := s.DeleteTag(bounded(t), expected); !errors.Is(err, wantErr) || got.ID != 0 {
				t.Fatal(got, err)
			}
			if after := tagPreview(t, s, 1); !reflect.DeepEqual(after, before) {
				t.Fatal(after, before)
			}
		})
	}
}

func TestTagChangesSQLRefusalAndAdmission(t *testing.T) {
	for _, operation := range []string{"UPDATE", "DELETE", "CASCADE"} {
		for _, refusal := range []string{"RAISE(ABORT,'test')", "RAISE(IGNORE)"} {
			t.Run(operation+refusal, func(t *testing.T) {
				s, _, _, _ := managementReadFixture(t)
				expected := tagPreview(t, s, 1)
				triggerOperation, table := operation, "tags"
				if operation == "CASCADE" {
					triggerOperation, table = "DELETE", "work_tags"
				}
				if _, err := s.db.Exec("CREATE TRIGGER reject_tag BEFORE " + triggerOperation + " ON " + table + " BEGIN SELECT " + refusal + "; END"); err != nil {
					t.Fatal(err)
				}
				change := func(ctx context.Context) error {
					var got manga.Tag
					var err error
					if operation == "UPDATE" {
						got, err = s.RenameTag(ctx, expected.Tag, "変更")
					} else {
						got, err = s.DeleteTag(ctx, expected)
					}
					if got.ID != 0 {
						t.Fatal(got, err)
					}
					return err
				}
				if err := change(bounded(t)); err == nil || errors.Is(err, ErrTagChangeOutcomeUnknown) || errors.Is(err, ErrDBStopped) {
					t.Fatal(err)
				}
				if after := tagPreview(t, s, 1); !reflect.DeepEqual(after, expected) {
					t.Fatal(after, expected)
				}
				cancelled, cancel := context.WithCancel(bounded(t))
				cancel()
				if err := change(cancelled); !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				s.admission.stop()
				if err := change(bounded(t)); !errors.Is(err, ErrDBStopped) {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestTagChangeCleanupOutcomes(t *testing.T) {
	for _, deleting := range []bool{false, true} {
		for _, commit := range []bool{false, true} {
			t.Run(fmt.Sprint(deleting, commit), func(t *testing.T) {
				s, path, _, _ := managementReadFixture(t)
				expected := tagPreview(t, s, 2) // Unused: this fault driver does not enable FKs.
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
				name := fmt.Sprintf("tag-change-fault-%d", faultDriverID.Add(1))
				sql.Register(name, cleanupDriver{commit: commit})
				db, err := sql.Open(name, path)
				if err != nil {
					t.Fatal(err)
				}
				db.SetMaxOpenConns(1)
				faulty := &Store{db: db}
				defer faulty.Close()
				if commit && !deleting {
					if got, err := faulty.RenameTag(bounded(t), expected.Tag, string(expected.Tag.Name)); err != nil || got != expected.Tag {
						t.Fatal("no-op must not commit", got, err)
					}
				}
				if !commit {
					expected.Tag.Name = "古い確認"
				}
				var got manga.Tag
				if deleting {
					got, err = faulty.DeleteTag(bounded(t), expected)
				} else {
					got, err = faulty.RenameTag(bounded(t), expected.Tag, "確定した改名")
				}
				if got.ID != 0 || !errors.Is(err, syntheticCleanup) || !errors.Is(err, ErrDBStopped) || errors.Is(err, ErrTagChangeOutcomeUnknown) != commit || errors.Is(err, ErrTagChangeCommitted) {
					t.Fatal(got, err)
				}
				if _, err := faulty.ListTags(bounded(t)); !errors.Is(err, ErrDBStopped) {
					t.Fatal(err)
				}
				reader, err := Open(bounded(t), path) // Observation only; never a mutation retry.
				if err != nil {
					t.Fatal(err)
				}
				defer reader.Close()
				preview, err := reader.PreviewTagDelete(bounded(t), 2)
				if commit && deleting {
					if !errors.Is(err, ErrTagNotFound) {
						t.Fatal(preview, err)
					}
				} else if err != nil || (commit && preview.Tag.Name != "確定した改名") || (!commit && preview.Tag.Name != "未使用") {
					t.Fatal(preview, err)
				}
			})
		}
	}
	tag := manga.Tag{ID: 1, Name: "確定済み"}
	for _, committed := range []bool{false, true} {
		got, err := tagChangeOutcome(tag, errors.Join(ErrDBStopped, sql.ErrConnDone), committed)
		if !errors.Is(err, ErrDBStopped) || errors.Is(err, ErrTagChangeCommitted) != committed || (committed && got != tag) || (!committed && got.ID != 0) || errors.Is(err, ErrTagChangeOutcomeUnknown) {
			t.Fatal(got, err)
		}
	}
}

func TestTagChangeBusyCommitStopsWithoutRetry(t *testing.T) {
	for _, deleting := range []bool{false, true} {
		t.Run(fmt.Sprint(deleting), func(t *testing.T) {
			s, path, _, _ := managementReadFixture(t)
			expected := tagPreview(t, s, 1)
			if _, err := s.db.Exec("PRAGMA journal_mode=DELETE; PRAGMA busy_timeout=0"); err != nil {
				t.Fatal(err)
			}
			observer, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer observer.Close()
			read, err := observer.BeginTx(bounded(t), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer read.Rollback()
			var name string
			if err := read.QueryRow("SELECT name FROM tags WHERE id=1").Scan(&name); err != nil {
				t.Fatal(err)
			}
			var got manga.Tag
			if deleting {
				got, err = s.DeleteTag(bounded(t), expected)
			} else {
				got, err = s.RenameTag(bounded(t), expected.Tag, "変更")
			}
			if got.ID != 0 || !errors.Is(err, ErrTagChangeOutcomeUnknown) || !errors.Is(err, ErrDBStopped) || errors.Is(err, ErrTagChangeCommitted) {
				t.Fatal(got, err)
			}
			if _, err := s.ListTags(bounded(t)); !errors.Is(err, ErrDBStopped) {
				t.Fatal(err)
			}
			if err := read.Rollback(); err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(bounded(t), path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			if got := tagPreview(t, reopened, 1); !reflect.DeepEqual(got, expected) {
				t.Fatal(got, expected)
			}
		})
	}
}

func TestTagDeleteConcurrentWriterCannotExpandTargets(t *testing.T) {
	s, path, w, _ := managementReadFixture(t)
	w.ID, w.URL, w.LastAccessedAt = 0, "https://example.com/added-after-confirmation", nil
	w, err := s.CreateWork(bounded(t), w)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		t.Fatal(err)
	}
	expected := tagPreview(t, s, 1)
	called := false
	name := fmt.Sprintf("tag-delete-writer-%d", faultDriverID.Add(1))
	sql.Register(name, previewStepDriver{afterFirst: func() {
		called = true
		// The writer commits while the delete transaction still sees its old snapshot.
		if _, err := s.EditWork(bounded(t), w.ID, withTags(w, nil, "共有"), w.UpdatedAt); err != nil {
			t.Fatal(err)
		}
	}})
	db, err := sql.Open(name, path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	writer := &Store{db: db}
	defer writer.Close()
	if got, err := writer.DeleteTag(bounded(t), expected); !called || err == nil || got.ID != 0 || errors.Is(err, ErrTagChangeOutcomeUnknown) {
		t.Fatal(got, called, err)
	}
	if got := tagPreview(t, s, 1); got.Tag != expected.Tag || len(got.Works) != 3 {
		t.Fatal(got)
	}
}
