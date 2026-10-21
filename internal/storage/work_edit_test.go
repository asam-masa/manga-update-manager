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

func textOf(w manga.Work) manga.EditWorkInput {
	return manga.EditWorkInput{URL: w.URL, Title: w.Title, SiteName: w.SiteName, Notes: w.Notes}
}

func TestEditWorkPreservesFieldsAndReopens(t *testing.T) {
	s, path := managedRecordStore(t)
	w := coverReferenceWork(t, s)
	readyCover(t, s, testCoverID)
	var err error
	w, _, err = s.SetWorkCover(context.Background(), w.ID, testCoverID, w.UpdatedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	beforeRecords, err := s.ListManagedCovers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	input := manga.EditWorkInput{URL: " https://example.com/new ", Title: " New title ", SiteName: " New site ", Notes: " New notes "}
	// The input contains no stale timestamps; preserve access recorded afterward.
	access := w.CreatedAt.Add(2 * time.Hour)
	if err := s.RecordLastAccess(context.Background(), w.ID, access); err != nil {
		t.Fatal(err)
	}
	w.LastAccessedAt = &access
	for _, backwards := range []bool{false, true} {
		at := w.UpdatedAt.Add(time.Hour).In(time.FixedZone("test", 9*3600))
		if backwards {
			at = w.CreatedAt.Add(-time.Hour)
			input.Title = "Backwards clock"
		}
		want := w
		normalized, _ := input.Normalize()
		want.URL, want.Title, want.SiteName, want.Notes = normalized.URL, normalized.Title, normalized.SiteName, normalized.Notes
		if !backwards {
			want.UpdatedAt = at.UTC()
		}
		got, err := s.EditWork(context.Background(), w.ID, input, at)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal(got, want, err)
		}
		w = want
	}
	// Even a later clock and whitespace-only changes do not execute UPDATE.
	if _, err := s.db.Exec("CREATE TRIGGER reject_noop BEFORE UPDATE ON works BEGIN SELECT RAISE(ABORT,'unexpected UPDATE'); END;"); err != nil {
		t.Fatal(err)
	}
	input = textOf(w)
	input.Title = " " + input.Title + " "
	got, err := s.EditWork(context.Background(), w.ID, input, w.UpdatedAt.Add(time.Hour))
	if err != nil || !reflect.DeepEqual(got, w) {
		t.Fatal(got, err)
	}
	afterRecords, err := s.ListManagedCovers(context.Background())
	if err != nil || !reflect.DeepEqual(beforeRecords, afterRecords) {
		t.Fatal(afterRecords, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err = reopened.WorkByID(context.Background(), w.ID)
	if err != nil || !reflect.DeepEqual(got, w) {
		t.Fatal(got, w, err)
	}
}

func TestEditWorkRefusalsPreserveData(t *testing.T) {
	s, _ := managedRecordStore(t)
	w := coverReferenceWork(t, s)
	other, err := manga.NewWork(manga.NewWorkInput{URL: "https://example.com/other", Title: "Other"}, w.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateWork(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id    int64
		input manga.EditWorkInput
		at    time.Time
		want  error
	}{
		{w.ID, manga.EditWorkInput{URL: other.URL, Title: "Duplicate"}, w.UpdatedAt, manga.ErrDuplicateURL},
		{w.ID, manga.EditWorkInput{URL: "file:///x", Title: "Bad"}, w.UpdatedAt, manga.ErrURLScheme},
		{-1, textOf(w), w.UpdatedAt, ErrWorkNotFound},
		{w.ID, textOf(w), time.Time{}, manga.ErrTimestampInvalid},
	} {
		got, err := s.EditWork(context.Background(), tc.id, tc.input, tc.at)
		if !errors.Is(err, tc.want) || got.ID != 0 {
			t.Fatal(got, err, tc.want)
		}
		after, err := s.WorkByID(context.Background(), w.ID)
		if err != nil || !reflect.DeepEqual(after, w) {
			t.Fatal(after, err)
		}
	}
	// Optional fields can be cleared, without replacing non-editable fields.
	input := textOf(w)
	input.SiteName, input.Notes = "", ""
	want := w
	want.SiteName, want.Notes = "", ""
	got, err := s.EditWork(context.Background(), w.ID, input, w.UpdatedAt)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, want, err)
	}
}

func TestEditWorkUpdateFailures(t *testing.T) {
	for _, action := range []string{"RAISE(ABORT,'synthetic failure')", "RAISE(IGNORE)"} {
		t.Run(action, func(t *testing.T) {
			s, _ := managedRecordStore(t)
			w := coverReferenceWork(t, s)
			if _, err := s.db.Exec("CREATE TRIGGER reject_edit BEFORE UPDATE ON works BEGIN SELECT " + action + "; END;"); err != nil {
				t.Fatal(err)
			}
			input := textOf(w)
			input.Title = "Changed"
			got, err := s.EditWork(context.Background(), w.ID, input, w.UpdatedAt.Add(time.Hour))
			if err == nil || got.ID != 0 || errors.Is(err, ErrWorkEditOutcomeUnknown) {
				t.Fatal(got, err)
			}
			after, err := s.WorkByID(context.Background(), w.ID)
			if err != nil || !reflect.DeepEqual(after, w) {
				t.Fatal(after, err)
			}
		})
	}
}

func TestEditWorkCancellationAndStop(t *testing.T) {
	s, _ := managedRecordStore(t)
	w := coverReferenceWork(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := s.EditWork(ctx, w.ID, textOf(w), w.UpdatedAt); !errors.Is(err, context.Canceled) || got.ID != 0 {
		t.Fatal(got, err)
	}
	s.admission.stop()
	if got, err := s.EditWork(context.Background(), w.ID, textOf(w), w.UpdatedAt); !errors.Is(err, ErrDBStopped) || got.ID != 0 {
		t.Fatal(got, err)
	}
}

func TestEditWorkCleanupFaults(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(fmt.Sprint(commit), func(t *testing.T) {
			original, path := managedRecordStore(t)
			w := coverReferenceWork(t, original)
			if err := original.Close(); err != nil {
				t.Fatal(err)
			}
			name := fmt.Sprintf("edit-fault-%d", faultDriverID.Add(1))
			sql.Register(name, cleanupDriver{commit: commit})
			db, err := sql.Open(name, path)
			if err != nil {
				t.Fatal(err)
			}
			db.SetMaxOpenConns(1)
			s := &Store{db: db}
			defer s.Close()
			input := textOf(w)
			if commit {
				// A no-op must not call even a Commit that would return an error.
				got, err := s.EditWork(bounded(t), w.ID, input, w.UpdatedAt.Add(time.Hour))
				if err != nil || !reflect.DeepEqual(got, w) {
					t.Fatal("no-op committed", got, err)
				}
			}
			if commit {
				input.Title = "Committed despite returned error"
			}
			got, err := s.EditWork(bounded(t), w.ID, input, w.UpdatedAt.Add(time.Hour))
			if !errors.Is(err, syntheticCleanup) || !errors.Is(err, ErrDBStopped) || errors.Is(err, ErrWorkEditOutcomeUnknown) != commit || got.ID != 0 {
				t.Fatal(got, err)
			}
			if _, err := s.ListWorks(context.Background()); !errors.Is(err, ErrDBStopped) {
				t.Fatal(err)
			}
			// Observe the persisted outcome independently, not an automatic retry.
			reader, err := Open(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			after, err := reader.WorkByID(context.Background(), w.ID)
			want := w
			if commit {
				want.Title = input.Title
				want.UpdatedAt = w.UpdatedAt.Add(time.Hour)
			}
			if err != nil || !reflect.DeepEqual(after, want) {
				t.Fatal(after, want, err)
			}
		})
	}
}
