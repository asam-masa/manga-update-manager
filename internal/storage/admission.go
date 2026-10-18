package storage

import (
	"context"
	"database/sql"
	"errors"
	"sync"
)

// ErrDBStopped rejects further SQL on this Store. It does not mean rollback.
var ErrDBStopped = errors.New("database operations stopped")

type dbAdmission struct {
	once    sync.Once
	mu      sync.Mutex
	stopped bool
	done    chan struct{}
	slot    chan struct{}
}

func (g *dbAdmission) init() {
	g.once.Do(func() { g.done = make(chan struct{}); g.slot = make(chan struct{}, 1) })
}

func (g *dbAdmission) stop() {
	g.init()
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.stopped {
		g.stopped = true
		close(g.done)
	}
}

// begin's final check is the execution-start boundary, after pool acquisition.
// Keep the lease through result consumption and transaction cleanup. No re-entry.
func (s *Store) begin(ctx context.Context) (*sql.Conn, func(*error), error) {
	g := &s.admission
	g.init()
	select {
	case <-g.done:
		return nil, nil, ErrDBStopped
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case g.slot <- struct{}{}:
	}
	// select may choose a free slot even when done is already closed.
	// Reject before touching the pool, including after Store.Close completed.
	g.mu.Lock()
	stopped := g.stopped
	g.mu.Unlock()
	if stopped {
		<-g.slot
		return nil, nil, ErrDBStopped
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		<-g.slot
		return nil, nil, err
	}
	finish := func(result *error) {
		if errors.Is(*result, ErrDBStopped) {
			g.stop()
		}
		if err := conn.Close(); err != nil {
			g.stop()
			*result = errors.Join(*result, ErrDBStopped, err)
		}
		<-g.slot
	}
	g.mu.Lock()
	if g.stopped {
		err = ErrDBStopped
	} else {
		err = ctx.Err()
	}
	g.mu.Unlock()
	if err != nil {
		finish(&err)
		return nil, nil, err
	}
	return conn, finish, nil
}
