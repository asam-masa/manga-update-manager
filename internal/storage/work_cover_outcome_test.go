package storage

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

func TestWorkCoverCommitErrorIsUnknown(t *testing.T) {
	for _, remove := range []bool{false, true} {
		name := "set"
		if remove {
			name = "remove"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			store, path := managedRecordStore(t)
			work := coverReferenceWork(t, store)
			readyCover(t, store, testCoverID)
			if _, err := store.db.ExecContext(ctx, "PRAGMA journal_mode=DELETE; PRAGMA busy_timeout=0"); err != nil {
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
			if err := read.QueryRowContext(ctx, "SELECT title FROM works WHERE id=?", work.ID).Scan(&title); err != nil {
				t.Fatal(err)
			}
			var got manga.Work
			var old string
			if remove {
				got, old, err = store.RemoveWorkCover(ctx, work.ID, work.UpdatedAt.Add(time.Hour))
			} else {
				got, old, err = store.SetWorkCover(ctx, work.ID, testCoverID, work.UpdatedAt.Add(time.Hour))
			}
			var cause *sqlite.Error
			if !errors.Is(err, ErrWorkCoverOutcomeUnknown) || !errors.As(err, &cause) || cause.Code() != sqlite3.SQLITE_BUSY {
				t.Fatalf("missing outcome or cause: %v", err)
			}
			if old != "" || !reflect.DeepEqual(got, manga.Work{}) {
				t.Fatal("commit error returned success data")
			}
			if err := read.Rollback(); err != nil {
				t.Fatal(err)
			}
			if _, err := store.WorkByID(ctx, work.ID); !errors.Is(err, ErrDBStopped) {
				t.Fatalf("commit failure did not stop DB: %v", err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			after, err := reopened.WorkByID(ctx, work.ID)
			if err != nil || !reflect.DeepEqual(after, work) {
				t.Fatal("busy fixture changed persisted work")
			}
		})
	}
}

func TestWorkCoverCommitDeadlineStopsWithoutSuccess(t *testing.T) {
	store, path := managedRecordStore(t)
	work := coverReferenceWork(t, store)
	readyCover(t, store, testCoverID)
	if _, err := store.db.Exec("PRAGMA journal_mode=DELETE; PRAGMA busy_timeout=10000"); err != nil {
		t.Fatal(err)
	}
	observer, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	read, err := observer.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Rollback()
	var title string
	if err := read.QueryRow("SELECT title FROM works WHERE id=?", work.ID).Scan(&title); err != nil {
		t.Fatal(err)
	}
	// The reader permits UPDATE but holds a SHARED lock across COMMIT.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	started := time.Now()
	got, old, err := store.SetWorkCover(ctx, work.ID, testCoverID, work.UpdatedAt.Add(time.Hour))
	if elapsed := time.Since(started); elapsed >= 5*time.Second {
		t.Fatalf("commit waited for busy_timeout instead of context: %v", elapsed)
	}
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, ErrWorkCoverOutcomeUnknown) || !errors.Is(err, ErrDBStopped) {
		t.Fatalf("deadline lost outcome or stop classification: %v", err)
	}
	if old != "" || !reflect.DeepEqual(got, manga.Work{}) {
		t.Fatal("deadline returned success data")
	}
	if _, err := store.WorkByID(context.Background(), work.ID); !errors.Is(err, ErrDBStopped) {
		t.Fatalf("commit deadline did not stop DB: %v", err)
	}
	if err := read.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	after, err := reopened.WorkByID(context.Background(), work.ID)
	if err != nil || !reflect.DeepEqual(after, work) {
		t.Fatalf("fixture work changed: %#v %v", after, err)
	}
	cover, err := reopened.ManagedCoverByID(context.Background(), testCoverID)
	if err != nil || cover.State != "ready" || cover.Info == nil || !reflect.DeepEqual(*cover.Info, testManagedInfo()) {
		t.Fatalf("candidate cover record was lost: %#v %v", cover, err)
	}
}

func TestWorkCoverPreCommitRejectionIsNotUnknown(t *testing.T) {
	store, _ := managedRecordStore(t)
	work := coverReferenceWork(t, store)
	_, _, err := store.SetWorkCover(context.Background(), work.ID, "../invalid", work.UpdatedAt)
	if !errors.Is(err, ErrInvalidManagedCover) || errors.Is(err, ErrWorkCoverOutcomeUnknown) {
		t.Fatalf("input rejection misclassified: %v", err)
	}
	_, _, err = store.RemoveWorkCover(context.Background(), work.ID, time.Time{})
	if !errors.Is(err, manga.ErrTimestampInvalid) || errors.Is(err, ErrWorkCoverOutcomeUnknown) {
		t.Fatalf("timestamp rejection misclassified: %v", err)
	}
}
