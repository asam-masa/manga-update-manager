package storage

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
)

func coverReferenceWork(t *testing.T, store *Store) manga.Work {
	t.Helper()
	work, err := manga.NewWork(manga.NewWorkInput{URL: "https://example.com/cover-test", Title: "Synthetic", Notes: "unchanged", ThumbnailPath: "legacy/image.png"}, time.Date(2026, 10, 13, 1, 2, 3, 123456789, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	work, err = store.CreateWork(context.Background(), work)
	if err != nil {
		t.Fatal(err)
	}
	access := work.CreatedAt.Add(time.Minute)
	if err := store.RecordLastAccess(context.Background(), work.ID, access); err != nil {
		t.Fatal(err)
	}
	work.LastAccessedAt = &access
	return work
}

func readyCover(t *testing.T, store *Store, id string) {
	t.Helper()
	if err := store.ReserveManagedCover(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if err := store.ConfirmManagedCover(context.Background(), id, testManagedInfo()); err != nil {
		t.Fatal(err)
	}
}

func TestWorkCoverLifecycleAndReopen(t *testing.T) {
	ctx := context.Background()
	store, path := managedRecordStore(t)
	original := coverReferenceWork(t, store)
	readyCover(t, store, testCoverID)
	secondID := strings.Repeat("b", 32)
	readyCover(t, store, secondID)
	at := original.UpdatedAt.Add(time.Hour).In(time.FixedZone("test", 9*3600))
	want := original
	want.ThumbnailPath, want.UpdatedAt = "covers/"+testCoverID+".png", at.UTC()
	got, old, err := store.SetWorkCover(ctx, original.ID, testCoverID, at)
	if err != nil || old != original.ThumbnailPath || !reflect.DeepEqual(got, want) {
		t.Fatalf("set: %#v %q %v", got, old, err)
	}
	// Repeating the same reference does not change the modification timestamp.
	got, old, err = store.SetWorkCover(ctx, original.ID, testCoverID, at.Add(time.Hour))
	if err != nil || old != want.ThumbnailPath || !reflect.DeepEqual(got, want) {
		t.Fatalf("repeat: %#v %q %v", got, old, err)
	}
	previous := want.ThumbnailPath
	want.ThumbnailPath = "covers/" + secondID + ".png"
	got, old, err = store.SetWorkCover(ctx, original.ID, secondID, original.CreatedAt.Add(-time.Hour))
	if err != nil || old != previous || !reflect.DeepEqual(got, want) {
		t.Fatalf("backwards clock: %#v %q %v", got, old, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err = reopened.WorkByID(ctx, original.ID)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("reopen: %#v %v", got, err)
	}
	previous = want.ThumbnailPath
	want.ThumbnailPath, want.UpdatedAt = "", at.UTC().Add(time.Hour)
	got, old, err = reopened.RemoveWorkCover(ctx, original.ID, want.UpdatedAt)
	if err != nil || old != previous || !reflect.DeepEqual(got, want) {
		t.Fatalf("remove: %#v %q %v", got, old, err)
	}
	got, old, err = reopened.RemoveWorkCover(ctx, original.ID, at.Add(2*time.Hour))
	if err != nil || old != "" || !reflect.DeepEqual(got, want) {
		t.Fatalf("empty remove: %#v %q %v", got, old, err)
	}
	for _, id := range []string{testCoverID, secondID} {
		cover, err := reopened.ManagedCoverByID(ctx, id)
		if err != nil || cover.State != "ready" || cover.Info == nil || *cover.Info != testManagedInfo() {
			t.Fatalf("record changed: %#v %v", cover, err)
		}
	}
	var count int
	if err := reopened.db.QueryRow("SELECT count(*) FROM managed_covers").Scan(&count); err != nil || count != 2 {
		t.Fatalf("legacy path enrolled: %d %v", count, err)
	}
}

func TestWorkCoverRejectedChangesPreserveState(t *testing.T) {
	ctx := context.Background()
	store, _ := managedRecordStore(t)
	work := coverReferenceWork(t, store)
	readyCover(t, store, testCoverID)
	reserved := strings.Repeat("b", 32)
	if err := store.ReserveManagedCover(ctx, reserved); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		id       int64
		cover    string
		at       time.Time
		expected error
	}{
		{"invalid ID", work.ID, "../" + testCoverID, work.UpdatedAt, ErrInvalidManagedCover},
		{"missing work", -1, testCoverID, work.UpdatedAt, ErrWorkNotFound},
		{"missing cover", work.ID, strings.Repeat("c", 32), work.UpdatedAt, ErrManagedCoverNotFound},
		{"reserved", work.ID, reserved, work.UpdatedAt, ErrInvalidManagedCover},
		{"zero time", work.ID, testCoverID, time.Time{}, manga.ErrTimestampInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, old, err := store.SetWorkCover(ctx, tc.id, tc.cover, tc.at)
			if !errors.Is(err, tc.expected) || old != "" || !reflect.DeepEqual(got, manga.Work{}) {
				t.Fatalf("result: %#v %q %v", got, old, err)
			}
			after, err := store.WorkByID(ctx, work.ID)
			if err != nil || !reflect.DeepEqual(after, work) {
				t.Fatalf("work changed: %#v %v", after, err)
			}
		})
	}
	if _, err := store.db.Exec("PRAGMA ignore_check_constraints=ON; UPDATE managed_covers SET sha256=X'01' WHERE state='ready'; PRAGMA ignore_check_constraints=OFF;"); err != nil {
		t.Fatal(err)
	}
	got, old, err := store.SetWorkCover(ctx, work.ID, testCoverID, work.UpdatedAt.Add(time.Hour))
	if !errors.Is(err, ErrInvalidManagedCover) || old != "" || !reflect.DeepEqual(got, manga.Work{}) {
		t.Fatalf("damaged: %#v %q %v", got, old, err)
	}
	after, err := store.WorkByID(ctx, work.ID)
	if err != nil || !reflect.DeepEqual(after, work) {
		t.Fatal("damaged record changed work")
	}
}

func TestWorkCoverDatabaseFailures(t *testing.T) {
	for _, trigger := range []string{
		"BEFORE UPDATE OF thumbnail_path ON works BEGIN SELECT RAISE(ABORT,'synthetic work failure'); END;",
		"BEFORE UPDATE OF thumbnail_path ON works BEGIN SELECT RAISE(IGNORE); END;",
		"AFTER UPDATE OF thumbnail_path ON works BEGIN SELECT RAISE(ABORT,'synthetic after-update failure'); END;",
	} {
		t.Run(trigger, func(t *testing.T) {
			store, _ := managedRecordStore(t)
			work := coverReferenceWork(t, store)
			readyCover(t, store, testCoverID)
			if _, err := store.db.Exec("CREATE TRIGGER reject_work_cover " + trigger); err != nil {
				t.Fatal(err)
			}
			for _, remove := range []bool{false, true} {
				var got manga.Work
				var old string
				var err error
				if remove {
					got, old, err = store.RemoveWorkCover(context.Background(), work.ID, work.UpdatedAt.Add(time.Hour))
				} else {
					got, old, err = store.SetWorkCover(context.Background(), work.ID, testCoverID, work.UpdatedAt.Add(time.Hour))
				}
				if err == nil || old != "" || !reflect.DeepEqual(got, manga.Work{}) {
					t.Fatalf("failure reported success: %#v %q %v", got, old, err)
				}
				after, err := store.WorkByID(context.Background(), work.ID)
				if err != nil || !reflect.DeepEqual(after, work) {
					t.Fatal("failed update changed work")
				}
				cover, err := store.ManagedCoverByID(context.Background(), testCoverID)
				if err != nil || cover.Info == nil || *cover.Info != testManagedInfo() {
					t.Fatal("failed update changed record")
				}
			}
		})
	}
}

func TestWorkCoverCancellationAndClosedDatabase(t *testing.T) {
	ctx := context.Background()
	store, path := managedRecordStore(t)
	work := coverReferenceWork(t, store)
	readyCover(t, store, testCoverID)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	for _, closed := range []bool{false, true} {
		operationCtx := cancelled
		if closed {
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			operationCtx = ctx
		}
		for _, remove := range []bool{false, true} {
			var got manga.Work
			var old string
			var err error
			if remove {
				got, old, err = store.RemoveWorkCover(operationCtx, work.ID, work.UpdatedAt.Add(time.Hour))
			} else {
				got, old, err = store.SetWorkCover(operationCtx, work.ID, testCoverID, work.UpdatedAt.Add(time.Hour))
			}
			if err == nil || (!closed && !errors.Is(err, context.Canceled)) || old != "" || !reflect.DeepEqual(got, manga.Work{}) {
				t.Fatalf("invalid failure: %#v %q %v", got, old, err)
			}
		}
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	after, err := reopened.WorkByID(ctx, work.ID)
	if err != nil || !reflect.DeepEqual(after, work) {
		t.Fatal("failed operations changed persisted work")
	}
}

func TestWorkCoverInvalidStoredTimestamps(t *testing.T) {
	for _, stored := range []string{"invalid", "0001-01-01T00:00:00Z", "2020-01-01T00:00:00Z"} {
		t.Run(stored, func(t *testing.T) {
			store, _ := managedRecordStore(t)
			work := coverReferenceWork(t, store)
			readyCover(t, store, testCoverID)
			// Only the temporary fixture bypasses CHECK to emulate damaged data.
			if _, err := store.db.Exec("PRAGMA ignore_check_constraints=ON"); err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.Exec("UPDATE works SET updated_at=? WHERE id=?", stored, work.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.Exec("PRAGMA ignore_check_constraints=OFF"); err != nil {
				t.Fatal(err)
			}
			got, old, err := store.SetWorkCover(context.Background(), work.ID, testCoverID, work.UpdatedAt.Add(time.Hour))
			if err == nil || old != "" || !reflect.DeepEqual(got, manga.Work{}) {
				t.Fatalf("invalid stored time accepted: %#v %q %v", got, old, err)
			}
			var path, updated string
			if err := store.db.QueryRow("SELECT thumbnail_path,updated_at FROM works WHERE id=?", work.ID).Scan(&path, &updated); err != nil || path != work.ThumbnailPath || updated != stored {
				t.Fatal("invalid stored time repaired or reference changed")
			}
		})
	}
}
