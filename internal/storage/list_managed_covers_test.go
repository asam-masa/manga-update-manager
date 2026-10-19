package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestListManagedCoversOrderEmptyAndReopen(t *testing.T) {
	s, path := managedRecordStore(t)
	ctx := context.Background()
	if got, err := s.ListManagedCovers(ctx); err != nil || got == nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	ids := []string{strings.Repeat("c", 32), strings.Repeat("a", 32), strings.Repeat("b", 32)}
	for _, id := range ids {
		if err := s.ReserveManagedCover(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ConfirmManagedCover(ctx, ids[2], testManagedInfo()); err != nil {
		t.Fatal(err)
	}
	w := coverReferenceWork(t, s)
	if _, _, err := s.SetWorkCover(ctx, w.ID, ids[2], w.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	// Even referenced records and reservations are inventory, not candidates
	// authorized for deletion. Unknown files do not enroll themselves.
	unknown := filepath.Join(filepath.Dir(path), "unknown.png")
	if err := os.WriteFile(unknown, []byte("synthetic untouched file"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListManagedCovers(ctx)
	if err != nil || len(got) != 3 || got[0].ID != ids[1] || got[1].ID != ids[2] || got[2].ID != ids[0] {
		t.Fatal(got, err)
	}
	for _, c := range got {
		expected, err := s.ManagedCoverByID(ctx, c.ID)
		if err != nil || !reflect.DeepEqual(c, expected) {
			t.Fatal(c, expected, err)
		}
	}
	if got[0].State != "reserved" || got[0].Info != nil || got[1].State != "ready" || *got[1].Info != testManagedInfo() {
		t.Fatal(got)
	}
	before, err := s.WorkByID(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	again, err := reopened.ListManagedCovers(ctx)
	if err != nil || !reflect.DeepEqual(got, again) {
		t.Fatal(again, err)
	}
	after, err := reopened.WorkByID(ctx, w.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("inventory changed work", after, err)
	}
	data, err := os.ReadFile(unknown)
	if err != nil || string(data) != "synthetic untouched file" {
		t.Fatal("inventory changed file", err)
	}
}

func TestListManagedCoversRejectsPartialInventory(t *testing.T) {
	for _, update := range []string{
		"state='unknown'", "state='ready'", "id='FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF'",
		"volume_serial='ffffffffffffffff'", "state='ready',volume_serial='ffffffffffffffff',file_id='aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',byte_size=1,sha256=X'01'",
	} {
		t.Run(update, func(t *testing.T) {
			s, _ := managedRecordStore(t)
			ctx := context.Background()
			first, last := strings.Repeat("1", 32), strings.Repeat("f", 32)
			for _, id := range []string{first, last} {
				if err := s.ReserveManagedCover(ctx, id); err != nil {
					t.Fatal(err)
				}
			}
			// Inject invalid persisted state after a valid prefix in a disposable DB.
			if _, err := s.db.Exec("PRAGMA ignore_check_constraints=ON; UPDATE managed_covers SET " + update + " WHERE id='" + last + "'; PRAGMA ignore_check_constraints=OFF;"); err != nil {
				t.Fatal(err)
			}
			var before string
			query := "SELECT group_concat(quote(id)||quote(state)||quote(volume_serial)||quote(file_id)||quote(byte_size)||quote(sha256)) FROM managed_covers"
			if err := s.db.QueryRow(query).Scan(&before); err != nil {
				t.Fatal(err)
			}
			got, err := s.ListManagedCovers(ctx)
			if !errors.Is(err, ErrInvalidManagedCover) || got != nil {
				t.Fatal("partial inventory returned", got, err)
			}
			var after string
			if err := s.db.QueryRow(query).Scan(&after); err != nil || before != after {
				t.Fatal("damaged state repaired", err)
			}
			if _, err := s.ManagedCoverByID(ctx, first); err != nil {
				t.Fatal("ordinary rejection stopped Store", err)
			}
		})
	}
}

func TestListManagedCoversCancellationAndQueryFailure(t *testing.T) {
	s, _ := managedRecordStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := s.ListManagedCovers(ctx); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatal(got, err)
	}
	if _, err := s.ListManagedCovers(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("DROP TABLE managed_covers"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ListManagedCovers(context.Background()); err == nil || got != nil {
		t.Fatal(got, err)
	}
	if _, err := s.ListWorks(context.Background()); err != nil {
		t.Fatal("query rejection stopped Store", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ListManagedCovers(context.Background()); !errors.Is(err, ErrDBStopped) || got != nil {
		t.Fatal(got, err)
	}
}

func TestListManagedCoversRowsReleaseFailure(t *testing.T) {
	for _, withRecord := range []bool{false, true} {
		t.Run(fmt.Sprint(withRecord), func(t *testing.T) {
			original, path := managedRecordStore(t)
			if withRecord {
				if err := original.ReserveManagedCover(context.Background(), testCoverID); err != nil {
					t.Fatal(err)
				}
			}
			if err := original.Close(); err != nil {
				t.Fatal(err)
			}
			name := fmt.Sprintf("inventory-rows-fault-%d", faultDriverID.Add(1))
			sql.Register(name, cleanupDriver{rows: true})
			db, err := sql.Open(name, path)
			if err != nil {
				t.Fatal(err)
			}
			s := &Store{db: db}
			defer s.Close()
			got, err := s.ListManagedCovers(bounded(t))
			if !errors.Is(err, syntheticCleanup) || !errors.Is(err, ErrDBStopped) || got != nil {
				t.Fatal("release failure yielded inventory", got, err)
			}
			if _, err := s.ListWorks(context.Background()); !errors.Is(err, ErrDBStopped) {
				t.Fatal("release failure did not stop Store", err)
			}
		})
	}
}
