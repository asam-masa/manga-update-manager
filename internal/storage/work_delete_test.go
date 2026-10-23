package storage

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

func deleteInput(w manga.Work) manga.WorkDeleteInput {
	return manga.WorkDeleteInput{URL: w.URL, Title: w.Title, SiteName: w.SiteName, Notes: w.Notes, ThumbnailPath: w.ThumbnailPath}
}

func TestDeleteWorkPreservesOtherDataAndReopens(t *testing.T) {
	s, path := managedRecordStore(t)
	w := coverReferenceWork(t, s)
	readyCover(t, s, testCoverID)
	w, _, err := s.SetWorkCover(bounded(t), w.ID, testCoverID, w.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	other := w
	other.ID, other.URL, other.LastAccessedAt = 0, "https://example.com/shared", nil
	other, err = s.CreateWork(bounded(t), other)
	if err != nil {
		t.Fatal(err)
	}
	records, err := s.ListManagedCovers(bounded(t))
	if err != nil {
		t.Fatal(err)
	}
	// Synthetic files under t.TempDir, never user images. Storage must not touch
	// either the referenced managed PNG or an unknown/original image.
	picture := image.NewRGBA(image.Rect(0, 0, 2, 2))
	picture.Set(0, 0, color.RGBA{G: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatal(err)
	}
	files := []string{filepath.Join(filepath.Dir(path), "covers", testCoverID+".png"), filepath.Join(filepath.Dir(path), "original.png")}
	if err := os.MkdirAll(filepath.Dir(files[0]), 0700); err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if err := os.WriteFile(file, encoded.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	expected := deleteInput(w)
	// Access metadata after confirmation must not invalidate content confirmation.
	access := w.UpdatedAt.Add(time.Hour)
	if err := s.RecordLastAccess(bounded(t), w.ID, access); err != nil {
		t.Fatal(err)
	}
	w.LastAccessedAt = &access
	got, err := s.DeleteWork(bounded(t), w.ID, expected)
	if err != nil || !reflect.DeepEqual(got, w) {
		t.Fatal(got, err)
	}
	if _, err := s.DeleteWork(bounded(t), w.ID, expected); !errors.Is(err, ErrWorkNotFound) {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := Open(bounded(t), path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.WorkByID(bounded(t), w.ID); !errors.Is(err, ErrWorkNotFound) {
		t.Fatal(err)
	}
	after, err := r.WorkByID(bounded(t), other.ID)
	if err != nil || !reflect.DeepEqual(after, other) {
		t.Fatal(after, err)
	}
	afterRecords, err := r.ListManagedCovers(bounded(t))
	if err != nil || !reflect.DeepEqual(records, afterRecords) {
		t.Fatal(afterRecords, err)
	}
	for _, file := range files {
		got, err := os.ReadFile(file)
		if err != nil || !bytes.Equal(got, encoded.Bytes()) {
			t.Fatal(file, err)
		}
	}
}

func TestDeleteWorkCommitBusyIsUnknown(t *testing.T) {
	s, path := managedRecordStore(t)
	w := coverReferenceWork(t, s)
	ctx := bounded(t)
	if _, err := s.db.ExecContext(ctx, "PRAGMA journal_mode=DELETE; PRAGMA busy_timeout=0"); err != nil {
		t.Fatal(err)
	}
	observer, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	read, err := observer.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Rollback()
	var title string
	if err := read.QueryRowContext(ctx, "SELECT title FROM works WHERE id=?", w.ID).Scan(&title); err != nil {
		t.Fatal(err)
	}
	got, err := s.DeleteWork(ctx, w.ID, deleteInput(w))
	var cause *sqlite.Error
	if got.ID != 0 || !errors.Is(err, ErrWorkDeleteOutcomeUnknown) || !errors.Is(err, ErrDBStopped) || !errors.As(err, &cause) || cause.Code() != sqlite3.SQLITE_BUSY {
		t.Fatal(got, err)
	}
	if _, err := s.WorkByID(ctx, w.ID); !errors.Is(err, ErrDBStopped) {
		t.Fatal(err)
	}
	if err := read.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	after, err := r.WorkByID(ctx, w.ID)
	if err != nil || !reflect.DeepEqual(after, w) {
		t.Fatal(after, err)
	}
}

func TestDeleteWorkChecksExactCurrentValues(t *testing.T) {
	for _, field := range []string{"URL", "Title", "SiteName", "Notes", "ThumbnailPath"} {
		t.Run(field, func(t *testing.T) {
			s, _ := managedRecordStore(t)
			w := coverReferenceWork(t, s)
			expected := deleteInput(w)
			v := reflect.ValueOf(&expected).Elem().FieldByName(field)
			v.SetString(v.String() + " ") // No normalization of confirmation snapshots.
			got, err := s.DeleteWork(bounded(t), w.ID, expected)
			if !errors.Is(err, ErrWorkDeleteChanged) || got.ID != 0 {
				t.Fatal(got, err)
			}
			after, err := s.WorkByID(bounded(t), w.ID)
			if err != nil || !reflect.DeepEqual(after, w) {
				t.Fatal(after, err)
			}
		})
	}
	s, _ := managedRecordStore(t)
	w := coverReferenceWork(t, s)
	expected := deleteInput(w)
	input := textOf(w)
	input.Title = "B"
	if _, err := s.EditWork(bounded(t), w.ID, input, w.UpdatedAt.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteWork(bounded(t), w.ID, expected); !errors.Is(err, ErrWorkDeleteChanged) {
		t.Fatal(err)
	}
	if _, err := s.EditWork(bounded(t), w.ID, textOf(w), w.UpdatedAt.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	// A -> B -> A: later updated_at is deliberately not a rejection reason.
	if _, err := s.DeleteWork(bounded(t), w.ID, expected); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteWorkRejections(t *testing.T) {
	for _, action := range []string{"RAISE(ABORT,'synthetic failure')", "RAISE(IGNORE)"} {
		t.Run(action, func(t *testing.T) {
			s, _ := managedRecordStore(t)
			w := coverReferenceWork(t, s)
			if _, err := s.db.Exec("CREATE TRIGGER reject_delete BEFORE DELETE ON works BEGIN SELECT " + action + "; END"); err != nil {
				t.Fatal(err)
			}
			got, err := s.DeleteWork(bounded(t), w.ID, deleteInput(w))
			if err == nil || got.ID != 0 || errors.Is(err, ErrWorkDeleteOutcomeUnknown) || errors.Is(err, ErrDBStopped) {
				t.Fatal(got, err)
			}
			after, err := s.WorkByID(bounded(t), w.ID)
			if err != nil || !reflect.DeepEqual(after, w) {
				t.Fatal(after, err)
			}
		})
	}
	s, _ := managedRecordStore(t)
	w := coverReferenceWork(t, s)
	if got, err := s.DeleteWork(bounded(t), -1, deleteInput(w)); !errors.Is(err, ErrWorkNotFound) || got.ID != 0 {
		t.Fatal(got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := s.DeleteWork(ctx, w.ID, deleteInput(w)); !errors.Is(err, context.Canceled) || got.ID != 0 {
		t.Fatal(got, err)
	}
	s.admission.stop()
	if got, err := s.DeleteWork(bounded(t), w.ID, deleteInput(w)); !errors.Is(err, ErrDBStopped) || got.ID != 0 {
		t.Fatal(got, err)
	}
}

func TestDeleteWorkCleanupFaults(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(fmt.Sprint(commit), func(t *testing.T) {
			original, path := managedRecordStore(t)
			w := coverReferenceWork(t, original)
			if err := original.Close(); err != nil {
				t.Fatal(err)
			}
			name := fmt.Sprintf("delete-fault-%d", faultDriverID.Add(1))
			sql.Register(name, cleanupDriver{commit: commit})
			db, err := sql.Open(name, path)
			if err != nil {
				t.Fatal(err)
			}
			db.SetMaxOpenConns(1)
			s := &Store{db: db}
			defer s.Close()
			expected := deleteInput(w)
			if !commit {
				expected.Title = "Changed"
			} // Rollback failure after mismatch.
			got, err := s.DeleteWork(bounded(t), w.ID, expected)
			if got.ID != 0 || !errors.Is(err, syntheticCleanup) || !errors.Is(err, ErrDBStopped) || errors.Is(err, ErrWorkDeleteOutcomeUnknown) != commit || errors.Is(err, ErrWorkDeleteCommitted) {
				t.Fatal(got, err)
			}
			if _, err := s.ListWorks(bounded(t)); !errors.Is(err, ErrDBStopped) {
				t.Fatal(err)
			}
			r, err := Open(bounded(t), path) // Test-only observation, not a production retry.
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			after, err := r.WorkByID(bounded(t), w.ID)
			if commit {
				if !errors.Is(err, ErrWorkNotFound) {
					t.Fatal(after, err)
				}
			} else if err != nil || !reflect.DeepEqual(after, w) {
				t.Fatal(after, err)
			}
		})
	}
}

func TestDeleteWorkOutcomeAfterReleaseFailure(t *testing.T) {
	w := manga.Work{ID: 42, ThumbnailPath: "legacy/synthetic.png"}
	// Exercise the result classifier with the same stopped-error shape produced
	// by begin.finish. This is not a real connection/OS Close fault injection.
	err := errors.Join(ErrDBStopped, sql.ErrConnDone)
	for _, committed := range []bool{false, true} {
		got, gotErr := workDeleteOutcome(w, err, committed)
		if !errors.Is(gotErr, ErrDBStopped) || !errors.Is(gotErr, sql.ErrConnDone) || errors.Is(gotErr, ErrWorkDeleteCommitted) != committed || errors.Is(gotErr, ErrWorkDeleteOutcomeUnknown) {
			t.Fatal(got, gotErr)
		}
		if committed && !reflect.DeepEqual(got, w) || !committed && got.ID != 0 {
			t.Fatal(got)
		}
	}
}
