package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
	"modernc.org/sqlite"
)

func TestStoppedStoreRejectsEveryOperation(t *testing.T) {
	s, _ := managedRecordStore(t)
	w := coverReferenceWork(t, s)
	readyCover(t, s, testCoverID)
	cover, err := s.ManagedCoverByID(context.Background(), testCoverID)
	if err != nil {
		t.Fatal(err)
	}
	s.admission.stop()
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"create": func() error {
			candidate := w
			candidate.ID = 0
			candidate.LastAccessedAt = nil
			_, err := s.CreateWork(ctx, candidate)
			return err
		},
		"get":            func() error { _, err := s.WorkByID(ctx, 1); return err },
		"list":           func() error { _, err := s.ListWorks(ctx); return err },
		"access":         func() error { return s.RecordLastAccess(ctx, 1, time.Now()) },
		"edit":           func() error { _, err := s.EditWork(ctx, w.ID, textOf(w), time.Now()); return err },
		"settings-read":  func() error { _, err := s.DisplaySettings(ctx); return err },
		"settings-write": func() error { return s.SetRegistrationFormVisible(ctx, false) },
		"reserve":        func() error { return s.ReserveManagedCover(ctx, testCoverID) },
		"confirm":        func() error { return s.ConfirmManagedCover(ctx, testCoverID, ManagedCoverInfo{ByteSize: 1}) },
		"managed-get":    func() error { _, err := s.ManagedCoverByID(ctx, testCoverID); return err },
		"managed-list":   func() error { _, err := s.ListManagedCovers(ctx); return err },
		"cover-set":      func() error { _, _, err := s.SetWorkCover(ctx, 1, testCoverID, time.Now()); return err },
		"cover-remove":   func() error { _, _, err := s.RemoveWorkCover(ctx, 1, time.Now()); return err },
		"cleanup-check":  func() error { _, _, err := s.UnusedManagedCover(ctx, testCoverID); return err },
		"cleanup-forget": func() error { return s.ForgetUnusedManagedCover(ctx, cover) },
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, ErrDBStopped) {
				t.Fatalf("not stopped: %v", err)
			}
		})
	}
}

func TestRejectionsDoNotStopStore(t *testing.T) {
	s, _ := managedRecordStore(t)
	w := coverReferenceWork(t, s)
	duplicate := w
	duplicate.ID = 0
	duplicate.LastAccessedAt = nil
	if _, err := s.CreateWork(context.Background(), duplicate); !errors.Is(err, manga.ErrDuplicateURL) {
		t.Fatal(err)
	}
	if _, _, err := s.SetWorkCover(context.Background(), w.ID, "invalid", time.Now()); !errors.Is(err, ErrInvalidManagedCover) {
		t.Fatal(err)
	}
	// Rejection after BeginTx still has a successful explicit Rollback.
	if _, _, err := s.SetWorkCover(context.Background(), w.ID, testCoverID, time.Now()); !errors.Is(err, ErrManagedCoverNotFound) {
		t.Fatal(err)
	}
	if _, err := s.ListWorks(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func bounded(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestDBStopAfterPoolWaitAndCancelledWait(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(fmt.Sprint(stop), func(t *testing.T) {
			s, _ := managedRecordStore(t)
			ctx := bounded(t)
			held, err := s.db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer held.Close()
			waitCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := s.ListWorks(waitCtx); done <- err }()
			tick := time.NewTicker(time.Millisecond)
			defer tick.Stop()
			for s.db.Stats().WaitCount == 0 {
				select {
				case <-tick.C:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			if stop {
				s.admission.stop()
			} else {
				cancel()
			}
			if err := held.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				want := error(context.Canceled)
				if stop {
					want = ErrDBStopped
				}
				if !errors.Is(err, want) {
					t.Fatalf("got %v want %v", err, want)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if !stop {
				if _, err := s.ListWorks(ctx); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestCloseDrainsExistingLease(t *testing.T) {
	s, _ := managedRecordStore(t)
	ctx := bounded(t)
	conn, finish, err := s.begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Close() }()
	select {
	case <-s.admission.done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case err := <-done:
		t.Fatalf("closed with active lease: %v", err)
	default:
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO display_settings VALUES(1,1)"); err != nil {
		t.Fatal(err)
	}
	finish(&err)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := s.ListWorks(ctx); !errors.Is(err, ErrDBStopped) {
		t.Fatal(err)
	}
}

func TestConnectionReturnErrorStopsStore(t *testing.T) {
	s, _ := managedRecordStore(t)
	conn, finish, err := s.begin(bounded(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	original := errors.New("synthetic operation failure")
	err = original
	finish(&err) // Synthetic double-close exercises the same error branch.
	if !errors.Is(err, original) || !errors.Is(err, ErrDBStopped) || !errors.Is(err, sql.ErrConnDone) {
		t.Fatal(err)
	}
	if _, err := s.ListWorks(context.Background()); !errors.Is(err, ErrDBStopped) {
		t.Fatal(err)
	}
}

var faultDriverID atomic.Uint64
var syntheticCleanup = errors.New("synthetic transaction cleanup failure")

// Faults report failure AFTER real SQLite cleanup. They do not reproduce I/O loss.
type cleanupDriver struct{ commit, rows bool }

func (d cleanupDriver) Open(name string) (driver.Conn, error) {
	c, err := (&sqlite.Driver{}).Open(name)
	if err != nil {
		return nil, err
	}
	return cleanupConn{Conn: c, commit: d.commit, rows: d.rows}, nil
}

type cleanupConn struct {
	driver.Conn
	commit bool
	rows   bool
}

func (c cleanupConn) Prepare(query string) (driver.Stmt, error) {
	stmt, err := c.Conn.Prepare(query)
	if err != nil || !c.rows {
		return stmt, err
	}
	return cleanupStmt{Stmt: stmt}, nil
}

type cleanupStmt struct{ driver.Stmt }

func (s cleanupStmt) Query(args []driver.Value) (driver.Rows, error) {
	rows, err := s.Stmt.Query(args)
	if err != nil {
		return nil, err
	}
	return cleanupRows{Rows: rows}, nil
}

type cleanupRows struct{ driver.Rows }

func (r cleanupRows) Close() error { return errors.Join(r.Rows.Close(), syntheticCleanup) }

func (c cleanupConn) Begin() (driver.Tx, error) {
	tx, err := c.Conn.Begin()
	if err != nil {
		return nil, err
	}
	return cleanupTx{Tx: tx, commit: c.commit}, nil
}

type cleanupTx struct {
	driver.Tx
	commit bool
}

func (tx cleanupTx) Commit() error {
	err := tx.Tx.Commit()
	if tx.commit {
		return errors.Join(err, syntheticCleanup)
	}
	return err
}
func (tx cleanupTx) Rollback() error {
	err := tx.Tx.Rollback()
	if !tx.commit {
		return errors.Join(err, syntheticCleanup)
	}
	return err
}

func TestTransactionCleanupFailureStopsStore(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(fmt.Sprint(commit), func(t *testing.T) {
			original, path := managedRecordStore(t)
			w := coverReferenceWork(t, original)
			if commit {
				readyCover(t, original, testCoverID)
			}
			if err := original.Close(); err != nil {
				t.Fatal(err)
			}
			name := fmt.Sprintf("cleanup-fault-%d", faultDriverID.Add(1))
			sql.Register(name, cleanupDriver{commit: commit})
			db, err := sql.Open(name, path)
			if err != nil {
				t.Fatal(err)
			}
			db.SetMaxOpenConns(1)
			s := &Store{db: db}
			defer s.Close()
			got, old, err := s.SetWorkCover(bounded(t), w.ID, testCoverID, w.UpdatedAt.Add(time.Hour))
			if !errors.Is(err, syntheticCleanup) || !errors.Is(err, ErrDBStopped) || errors.Is(err, ErrWorkCoverOutcomeUnknown) != commit {
				t.Fatal(err)
			}
			if got.ID != 0 || old != "" {
				t.Fatal("returned success on cleanup failure")
			}
			if !commit && !errors.Is(err, ErrManagedCoverNotFound) {
				t.Fatal("original rejection lost", err)
			}
			if _, err := s.ListWorks(context.Background()); !errors.Is(err, ErrDBStopped) {
				t.Fatal(err)
			}
		})
	}
}

func TestRowsAutoCloseFailureStopsStore(t *testing.T) {
	original, path := managedRecordStore(t)
	if err := original.Close(); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("rows-fault-%d", faultDriverID.Add(1))
	sql.Register(name, cleanupDriver{rows: true})
	db, err := sql.Open(name, path)
	if err != nil {
		t.Fatal(err)
	}
	s := &Store{db: db}
	defer s.Close()
	if _, err := s.ListWorks(bounded(t)); !errors.Is(err, syntheticCleanup) || !errors.Is(err, ErrDBStopped) {
		t.Fatal(err)
	}
	if err := s.SetRegistrationFormVisible(context.Background(), false); !errors.Is(err, ErrDBStopped) {
		t.Fatal(err)
	}
}

func TestSingleRowCloseFailureStopsStore(t *testing.T) {
	for _, kind := range []string{"work", "managed", "settings"} {
		t.Run(kind, func(t *testing.T) {
			original, path := managedRecordStore(t)
			w := coverReferenceWork(t, original)
			readyCover(t, original, testCoverID)
			if err := original.SetRegistrationFormVisible(context.Background(), true); err != nil {
				t.Fatal(err)
			}
			if err := original.Close(); err != nil {
				t.Fatal(err)
			}
			name := fmt.Sprintf("row-fault-%d", faultDriverID.Add(1))
			sql.Register(name, cleanupDriver{rows: true})
			db, err := sql.Open(name, path)
			if err != nil {
				t.Fatal(err)
			}
			s := &Store{db: db}
			defer s.Close()
			switch kind {
			case "work":
				_, err = s.WorkByID(bounded(t), w.ID)
			case "managed":
				_, err = s.ManagedCoverByID(bounded(t), testCoverID)
			case "settings":
				_, err = s.DisplaySettings(bounded(t))
			}
			if !errors.Is(err, syntheticCleanup) || !errors.Is(err, ErrDBStopped) {
				t.Fatal(err)
			}
			if err := s.SetRegistrationFormVisible(context.Background(), false); !errors.Is(err, ErrDBStopped) {
				t.Fatal(err)
			}
		})
	}
}

func TestStoreStopVersusExecutionStart(t *testing.T) {
	original, _ := managedRecordStore(t)
	ctx := bounded(t)
	for range 100 {
		s := &Store{db: original.db}
		launch, stopped := make(chan struct{}), make(chan struct{})
		done := make(chan error, 1)
		go func() { <-launch; done <- s.SetRegistrationFormVisible(ctx, false) }()
		go func() { <-launch; s.admission.stop(); close(stopped) }()
		close(launch)
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, ErrDBStopped) {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		select {
		case <-stopped:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		if _, err := s.ListWorks(ctx); !errors.Is(err, ErrDBStopped) {
			t.Fatal(err)
		}
	}
}
