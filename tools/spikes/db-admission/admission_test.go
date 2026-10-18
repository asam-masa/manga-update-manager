// Package admission is an isolated candidate, not the production Store.
package admission

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

var errStopped = errors.New("DB admission stopped")

type admission struct {
	mu      sync.Mutex
	stopped bool
	done    chan struct{}
	slot    chan struct{}
	waiting func() // test-only notification after observing a full serial slot
}

func newAdmission() *admission {
	return &admission{done: make(chan struct{}), slot: make(chan struct{}, 1)}
}

func (g *admission) stop() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.stopped {
		g.stopped = true
		close(g.done)
	}
}

// admitted and starting are test synchronization hooks, never production APIs.
// Callback scope includes all SQL, result consumption and transaction cleanup.
func (g *admission) run(ctx context.Context, db *sql.DB, admitted, starting func(), fn func(*sql.Conn) error) (err error) {
	select {
	case <-g.done:
		return errStopped
	case <-ctx.Done():
		return ctx.Err()
	case g.slot <- struct{}{}:
		return g.withSlot(ctx, db, admitted, starting, fn)
	default:
	}
	if g.waiting != nil {
		g.waiting()
	}
	select {
	case <-g.done:
		return errStopped
	case <-ctx.Done():
		return ctx.Err()
	case g.slot <- struct{}{}:
		return g.withSlot(ctx, db, admitted, starting, fn)
	}
}

func (g *admission) withSlot(ctx context.Context, db *sql.DB, admitted, starting func(), fn func(*sql.Conn) error) (err error) {
	defer func() { <-g.slot }()
	if admitted != nil {
		admitted()
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			g.stop()
			err = errors.Join(err, closeErr)
		}
	}()
	// The execution-start linearization point is AFTER pool acquisition.
	g.mu.Lock()
	if g.stopped {
		g.mu.Unlock()
		return errStopped
	}
	if err := ctx.Err(); err != nil {
		g.mu.Unlock()
		return err
	}
	if starting != nil {
		starting() // test hook must not block or re-enter the gate
	}
	g.mu.Unlock()
	// Do not hold mu during SQL or cleanup: fn may itself report unsafe state.
	return fn(conn)
}

func fixture(t *testing.T) (context.Context, *sql.DB, *admission) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "fixture.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, "CREATE TABLE records(value INTEGER)"); err != nil {
		t.Fatal(err)
	}
	return ctx, db, newAdmission()
}

func await(t *testing.T, ctx context.Context, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func result(t *testing.T, ctx context.Context, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		return ctx.Err()
	}
}

func TestStopDrainsRunningTransaction(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "commit"}[commit], func(t *testing.T) {
			ctx, db, gate := fixture(t)
			entered, finish := make(chan struct{}), make(chan struct{})
			done := make(chan error, 1)
			go func() {
				done <- gate.run(ctx, db, nil, nil, func(conn *sql.Conn) error {
					tx, err := conn.BeginTx(ctx, nil)
					if err != nil {
						return err
					}
					defer tx.Rollback()
					if _, err := tx.ExecContext(ctx, "INSERT INTO records VALUES(1)"); err != nil {
						return err
					}
					close(entered)
					select {
					case <-finish:
					case <-ctx.Done():
						return ctx.Err()
					}
					// Running work can report Stop itself, without recursive locking.
					gate.stop()
					if commit {
						return tx.Commit()
					}
					return tx.Rollback()
				})
			}()
			await(t, ctx, entered)
			waiting := make(chan error, 1)
			go func() {
				waiting <- gate.run(ctx, db, nil, nil, func(*sql.Conn) error { return errors.New("queued callback ran") })
			}()
			gate.stop()
			close(finish)
			if err := result(t, ctx, done); err != nil {
				t.Fatal(err)
			}
			if err := result(t, ctx, waiting); !errors.Is(err, errStopped) {
				t.Fatal(err)
			}
			for _, query := range []string{"SELECT COUNT(*) FROM records", "INSERT INTO records VALUES(2)"} {
				called := false
				err := gate.run(ctx, db, nil, nil, func(conn *sql.Conn) error {
					called = true
					_, err := conn.ExecContext(ctx, query)
					return err
				})
				if called || !errors.Is(err, errStopped) {
					t.Fatalf("post-stop SQL admitted: %v", err)
				}
			}
			// Out-of-band test inspection, NOT a proposed recovery read.
			var count int
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM records").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != map[bool]int{false: 0, true: 1}[commit] {
				t.Fatalf("count=%d", count)
			}
		})
	}
}

func TestStopAfterPoolWait(t *testing.T) {
	ctx, db, gate := fixture(t)
	held, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	admitted := make(chan struct{})
	done := make(chan error, 1)
	called := false
	go func() {
		done <- gate.run(ctx, db, func() { close(admitted) }, nil, func(*sql.Conn) error { called = true; return nil })
	}()
	await(t, ctx, admitted)
	awaitPoolWait(t, ctx, db)
	gate.stop()
	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
	if err := result(t, ctx, done); !errors.Is(err, errStopped) || called {
		t.Fatalf("pool wait bypass: %v", err)
	}
}

func awaitPoolWait(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for db.Stats().WaitCount == 0 {
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

func TestCancelledWaitDoesNotLeakSlot(t *testing.T) {
	ctx, db, gate := fixture(t)
	gate.slot <- struct{}{}
	cancelled, cancel := context.WithCancel(ctx)
	defer cancel()
	waiting := make(chan struct{})
	gate.waiting = func() { close(waiting) }
	serialDone := make(chan error, 1)
	go func() {
		serialDone <- gate.run(cancelled, db, nil, nil, func(*sql.Conn) error { return errors.New("cancelled callback ran") })
	}()
	await(t, ctx, waiting)
	cancel()
	if err := result(t, ctx, serialDone); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	gate.waiting = nil
	<-gate.slot
	held, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	poolCtx, poolCancel := context.WithCancel(ctx)
	defer poolCancel()
	admitted := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- gate.run(poolCtx, db, func() { close(admitted) }, nil, func(*sql.Conn) error { return errors.New("cancelled pool callback ran") })
	}()
	await(t, ctx, admitted)
	awaitPoolWait(t, ctx, db)
	poolCancel()
	if err := result(t, ctx, done); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gate.run(ctx, db, nil, nil, func(conn *sql.Conn) error {
		_, err := conn.ExecContext(ctx, "INSERT INTO records VALUES(3)")
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestStopVersusStart(t *testing.T) {
	ctx, db, _ := fixture(t)
	for range 100 {
		gate := newAdmission()
		launch := make(chan struct{})
		done, stopped := make(chan error, 1), make(chan struct{})
		started, called, startedAfterStop := false, false, false
		go func() {
			<-launch
			done <- gate.run(ctx, db, nil, func() { started = true; startedAfterStop = gate.stopped }, func(conn *sql.Conn) error {
				called = true
				_, err := conn.ExecContext(ctx, "INSERT INTO records VALUES(4)")
				return err
			})
		}()
		go func() { <-launch; gate.stop(); close(stopped) }()
		close(launch)
		err := result(t, ctx, done)
		await(t, ctx, stopped)
		if startedAfterStop || started != called || (started && err != nil) || (!started && !errors.Is(err, errStopped)) {
			t.Fatalf("start=%t callback=%t err=%v", started, called, err)
		}
		if err := gate.run(ctx, db, nil, nil, func(*sql.Conn) error { return errors.New("reopened admission") }); !errors.Is(err, errStopped) {
			t.Fatal(err)
		}
	}
}
