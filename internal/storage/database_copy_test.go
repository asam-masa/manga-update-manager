package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"modernc.org/sqlite"
)

func TestCopyDatabaseRoundTripAndRefusal(t *testing.T) {
	s, _, _, _ := managementReadFixture(t)
	ctx := bounded(t)
	if err := s.SetRegistrationFormVisible(ctx, false); err != nil {
		t.Fatal(err)
	}
	before, err := s.LoadLibrarySnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wantSettings, err := s.DisplaySettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "copy '日本.sqlite")
	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if err := s.CopyDatabase(ctx, path); err != nil {
		t.Fatal(err)
	}
	copyStore, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer copyStore.Close()
	got, err := copyStore.LoadLibrarySnapshot(ctx)
	if err != nil || !reflect.DeepEqual(got, before) {
		t.Fatal(got, err)
	}
	gotSettings, err := copyStore.DisplaySettings(ctx)
	if err != nil || gotSettings != wantSettings {
		t.Fatal(gotSettings, err)
	}
	var check string
	if err := copyStore.db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&check); err != nil || check != "ok" {
		t.Fatal(check, err)
	}
	if got, err := s.LoadLibrarySnapshot(ctx); err != nil || !reflect.DeepEqual(got, before) {
		t.Fatal("source changed", got, err)
	}
	nonempty := filepath.Join(t.TempDir(), "existing.sqlite")
	if err := os.WriteFile(nonempty, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.CopyDatabase(ctx, nonempty); err == nil {
		t.Fatal("nonempty target accepted")
	}
	if data, err := os.ReadFile(nonempty); err != nil || string(data) != "preserve" {
		t.Fatal("existing target changed", err)
	}
	if _, err := s.ListWorks(ctx); err != nil {
		t.Fatal("refusal stopped store", err)
	}
	for _, path := range []string{"", " \t"} {
		if err := s.CopyDatabase(ctx, path); !errors.Is(err, ErrInvalidDatabaseCopyTarget) {
			t.Fatal(err)
		}
	}
}

func TestCopyDatabaseCancelledAndStopped(t *testing.T) {
	s, _ := managedRecordStore(t)
	path := filepath.Join(t.TempDir(), "copy.sqlite")
	ctx, cancel := context.WithCancel(bounded(t))
	cancel()
	if err := s.CopyDatabase(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("cancelled call touched target", err)
	}
	if _, err := s.ListWorks(bounded(t)); err != nil {
		t.Fatal(err)
	}
	s.admission.stop()
	if err := s.CopyDatabase(bounded(t), path); !errors.Is(err, ErrDBStopped) {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("stopped call touched target", err)
	}
}

type copyHookDriver struct {
	exec func(context.Context, string, []driver.NamedValue) (driver.Result, error)
}

func (d copyHookDriver) Open(name string) (driver.Conn, error) {
	c, err := (&sqlite.Driver{}).Open(name)
	if err != nil {
		return nil, err
	}
	return copyHookConn{Conn: c, exec: d.exec}, nil
}

type copyHookConn struct {
	driver.Conn
	exec func(context.Context, string, []driver.NamedValue) (driver.Result, error)
}

func (c copyHookConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	return c.exec(ctx, q, args)
}

func TestCopyDatabaseExecFailureAndAdmission(t *testing.T) {
	for _, mode := range []string{"failure", "cancel-wait", "stop-wait"} {
		t.Run(mode, func(t *testing.T) {
			_, source := managedRecordStore(t)
			fault := errors.New("synthetic copy failure")
			calls := 0
			name := fmt.Sprintf("copy-hook-%d", faultDriverID.Add(1))
			sql.Register(name, copyHookDriver{exec: func(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
				calls++
				if q != "VACUUM INTO ?" || len(args) != 1 || args[0].Value != "prepared-target" {
					t.Error("unexpected execution", q, args)
				}
				return nil, fault
			}})
			db, err := sql.Open(name, source)
			if err != nil {
				t.Fatal(err)
			}
			db.SetMaxOpenConns(1)
			s := &Store{db: db}
			defer s.Close()
			ctx := bounded(t)
			if mode == "failure" {
				if err := s.CopyDatabase(ctx, "prepared-target"); !errors.Is(err, fault) {
					t.Fatal(err)
				}
				if calls != 1 {
					t.Fatal(calls)
				}
				if err := s.CheckAdmission(); err != nil {
					t.Fatal(err)
				}
				conn, finish, err := s.begin(ctx)
				if err != nil {
					t.Fatal("lease leaked", err)
				}
				_ = conn
				finish(&err)
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			_, finish, err := s.begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			waitCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- s.CopyDatabase(waitCtx, "prepared-target") }()
			if mode == "stop-wait" {
				s.admission.stop()
			} else {
				cancel()
			}
			finish(&err)
			select {
			case err := <-done:
				want := error(context.Canceled)
				if mode == "stop-wait" {
					want = ErrDBStopped
				}
				if !errors.Is(err, want) {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if calls != 0 {
				t.Fatal("SQL ran after rejection", calls)
			}
		})
	}
}
