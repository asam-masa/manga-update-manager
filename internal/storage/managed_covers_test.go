package storage

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func managedRecordStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "records.sqlite")
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if store != nil {
			store.Close()
		}
	})
	return store, path
}

func testManagedInfo() ManagedCoverInfo {
	info := ManagedCoverInfo{VolumeSerial: ^uint64(0), ByteSize: 123}
	for i := range info.FileID {
		info.FileID[i] = byte(255 - i)
	}
	for i := range info.SHA256 {
		info.SHA256[i] = byte(i)
	}
	return info
}

func TestManagedCoverRecordLifecycleAndReopen(t *testing.T) {
	ctx := context.Background()
	store, path := managedRecordStore(t)
	if err := store.ReserveManagedCover(ctx, testCoverID); err != nil {
		t.Fatal(err)
	}
	reserved, err := store.ManagedCoverByID(ctx, testCoverID)
	if err != nil || reserved.State != "reserved" || reserved.Info != nil || reserved.RelativePath != "covers/"+testCoverID+".png" {
		t.Fatalf("reservation: %#v %v", reserved, err)
	}
	if err := store.ReserveManagedCover(ctx, testCoverID); !errors.Is(err, ErrManagedCoverExists) {
		t.Fatalf("duplicate: %v", err)
	}
	unchanged, err := store.ManagedCoverByID(ctx, testCoverID)
	if err != nil || !reflect.DeepEqual(reserved, unchanged) {
		t.Fatal("duplicate changed reservation")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if store != nil {
			store.Close()
		}
	})
	got, err := store.ManagedCoverByID(ctx, testCoverID)
	if err != nil || !reflect.DeepEqual(reserved, got) {
		t.Fatal("reservation lost after reopen")
	}
	info := testManagedInfo()
	if err := store.ConfirmManagedCover(ctx, testCoverID, info); err != nil {
		t.Fatal(err)
	}
	ready, err := store.ManagedCoverByID(ctx, testCoverID)
	if err != nil || ready.State != "ready" || ready.Info == nil || *ready.Info != info {
		t.Fatalf("ready: %#v %v", ready, err)
	}
	changed := info
	changed.ByteSize++
	if err := store.ConfirmManagedCover(ctx, testCoverID, changed); !errors.Is(err, ErrManagedCoverNotReserved) {
		t.Fatalf("second confirmation: %v", err)
	}
	if err := store.ReserveManagedCover(ctx, testCoverID); !errors.Is(err, ErrManagedCoverExists) {
		t.Fatalf("ready duplicate: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if store != nil {
			store.Close()
		}
	})
	got, err = store.ManagedCoverByID(ctx, testCoverID)
	if err != nil || !reflect.DeepEqual(ready, got) {
		t.Fatal("ready information changed or lost")
	}
	missing := strings.Repeat("a", 32)
	if err := store.ConfirmManagedCover(ctx, missing, info); !errors.Is(err, ErrManagedCoverNotReserved) {
		t.Fatalf("missing confirmation: %v", err)
	}
	if got, err := store.ManagedCoverByID(ctx, missing); !errors.Is(err, ErrManagedCoverNotFound) || !reflect.DeepEqual(got, ManagedCover{}) {
		t.Fatalf("missing read: %#v %v", got, err)
	}
}

func TestManagedCoverInvalidInputAndWriteFailure(t *testing.T) {
	ctx := context.Background()
	store, _ := managedRecordStore(t)
	for _, id := range []string{"", strings.Repeat("a", 31), strings.Repeat("A", 32), strings.Repeat("z", 32), testCoverID + "\x00tail", "../" + testCoverID} {
		if err := store.ReserveManagedCover(ctx, id); !errors.Is(err, ErrInvalidManagedCover) {
			t.Fatalf("invalid reserve: %q %v", id, err)
		}
		if err := store.ConfirmManagedCover(ctx, id, testManagedInfo()); !errors.Is(err, ErrInvalidManagedCover) {
			t.Fatalf("invalid confirm: %q %v", id, err)
		}
		if got, err := store.ManagedCoverByID(ctx, id); !errors.Is(err, ErrInvalidManagedCover) || !reflect.DeepEqual(got, ManagedCover{}) {
			t.Fatalf("invalid read: %#v %v", got, err)
		}
	}
	if err := store.ReserveManagedCover(ctx, testCoverID); err != nil {
		t.Fatal(err)
	}
	before, err := store.ManagedCoverByID(ctx, testCoverID)
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int64{0, -1} {
		info := testManagedInfo()
		info.ByteSize = size
		if err := store.ConfirmManagedCover(ctx, testCoverID, info); !errors.Is(err, ErrInvalidManagedCover) {
			t.Fatalf("invalid size: %v", err)
		}
	}
	if _, err := store.db.Exec(`CREATE TRIGGER reject_cover_confirmation BEFORE UPDATE ON managed_covers BEGIN SELECT RAISE(ABORT,'synthetic confirmation failure'); END;`); err != nil {
		t.Fatal(err)
	}
	if err := store.ConfirmManagedCover(ctx, testCoverID, testManagedInfo()); err == nil || !strings.Contains(err.Error(), "synthetic confirmation failure") {
		t.Fatalf("expected DB failure: %v", err)
	}
	after, err := store.ManagedCoverByID(ctx, testCoverID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("failed confirmation changed reservation")
	}
	if _, err := store.db.Exec(`CREATE TRIGGER reject_cover_reservation BEFORE INSERT ON managed_covers BEGIN SELECT RAISE(ABORT,'synthetic reservation failure'); END;`); err != nil {
		t.Fatal(err)
	}
	if err := store.ReserveManagedCover(ctx, strings.Repeat("b", 32)); err == nil || !strings.Contains(err.Error(), "synthetic reservation failure") {
		t.Fatalf("expected reserve DB failure: %v", err)
	}
	if _, err := store.ManagedCoverByID(ctx, strings.Repeat("b", 32)); !errors.Is(err, ErrManagedCoverNotFound) {
		t.Fatal("failed reservation inserted row")
	}
}

func TestManagedCoverCancellationAndClosedDB(t *testing.T) {
	ctx := context.Background()
	store, path := managedRecordStore(t)
	if err := store.ReserveManagedCover(ctx, testCoverID); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := store.ReserveManagedCover(cancelled, strings.Repeat("a", 32)); !errors.Is(err, context.Canceled) {
		t.Fatalf("reserve cancellation: %v", err)
	}
	if err := store.ConfirmManagedCover(cancelled, testCoverID, testManagedInfo()); !errors.Is(err, context.Canceled) {
		t.Fatalf("confirm cancellation: %v", err)
	}
	if got, err := store.ManagedCoverByID(cancelled, testCoverID); !errors.Is(err, context.Canceled) || !reflect.DeepEqual(got, ManagedCover{}) {
		t.Fatalf("read cancellation: %#v %v", got, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.ReserveManagedCover(ctx, strings.Repeat("a", 32)); err == nil {
		t.Fatal("closed DB reserve succeeded")
	}
	if err := store.ConfirmManagedCover(ctx, testCoverID, testManagedInfo()); err == nil {
		t.Fatal("closed DB confirm succeeded")
	}
	if got, err := store.ManagedCoverByID(ctx, testCoverID); err == nil || !reflect.DeepEqual(got, ManagedCover{}) {
		t.Fatalf("closed DB read: %#v %v", got, err)
	}
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := store.ManagedCoverByID(ctx, testCoverID)
	if err != nil || got.State != "reserved" || got.Info != nil {
		t.Fatalf("failed writes changed reservation: %#v %v", got, err)
	}
	if _, err := store.ManagedCoverByID(ctx, strings.Repeat("a", 32)); !errors.Is(err, ErrManagedCoverNotFound) {
		t.Fatal("failed write inserted row")
	}
}

func TestManagedCoverReadRejectsDamagedStateWithoutRepair(t *testing.T) {
	for _, update := range []string{
		"state='unknown'", "sha256=X''", "volume_serial='ffffffffffffffff'",
		"state='ready'", "state='ready',volume_serial='ffffffffffffffff',file_id='aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',byte_size=1,sha256=X'01'",
		"state='ready',volume_serial='FFFFFFFFFFFFFFFF',file_id='aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',byte_size=1,sha256=zeroblob(32)",
		"state='ready',volume_serial='ffffffffffffffff',file_id='aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'||char(0),byte_size=1,sha256=zeroblob(32)",
	} {
		t.Run(update, func(t *testing.T) {
			store, _ := managedRecordStore(t)
			ctx := context.Background()
			if err := store.ReserveManagedCover(ctx, testCoverID); err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.Exec("PRAGMA ignore_check_constraints=ON; UPDATE managed_covers SET " + update + "; PRAGMA ignore_check_constraints=OFF;"); err != nil {
				t.Fatal(err)
			}
			var before string
			if err := store.db.QueryRow("SELECT quote(state)||quote(volume_serial)||quote(file_id)||quote(byte_size)||quote(sha256) FROM managed_covers").Scan(&before); err != nil {
				t.Fatal(err)
			}
			got, err := store.ManagedCoverByID(ctx, testCoverID)
			if !errors.Is(err, ErrInvalidManagedCover) || !reflect.DeepEqual(got, ManagedCover{}) {
				t.Fatalf("damaged state accepted: %#v %v", got, err)
			}
			if err := store.ConfirmManagedCover(ctx, testCoverID, testManagedInfo()); !errors.Is(err, ErrManagedCoverNotReserved) {
				t.Fatalf("damaged state confirmed: %v", err)
			}
			var after string
			if err := store.db.QueryRow("SELECT quote(state)||quote(volume_serial)||quote(file_id)||quote(byte_size)||quote(sha256) FROM managed_covers").Scan(&after); err != nil || before != after {
				t.Fatal("damaged data automatically repaired")
			}
		})
	}
}
