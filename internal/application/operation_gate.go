package application

import (
	"context"
	"errors"
	"sync"
)

var ErrOperationsPaused = errors.New("normal operations paused for backup")
var ErrOperationsClosing = errors.New("operations closing")
var ErrBackupBusy = errors.New("backup waiting or running")

// OperationGate orders entry admission, not DB/image stage permission. Its zero
// value is ready. Do not copy after use. Every admitted operation must remain
// active through all of its resource releases. Control calls (progress/cancel)
// must not acquire a normal ticket. No I/O or waits run under mu.
type OperationGate struct {
	mu      sync.Mutex
	active  int
	backup  *BackupOperation
	closing bool
	drained chan struct{}
}

// BackupOperation retains the pause through all backup work and resource release.
// Keep the pointer: copied or stale tickets cannot release another reservation.
type BackupOperation struct {
	owner *OperationGate
	ready chan struct{}
}

func (g *OperationGate) BeginNormal() (func(), error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closing {
		return nil, ErrOperationsClosing
	}
	if g.backup != nil {
		return nil, ErrOperationsPaused
	}
	g.active++
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			g.active--
			if g.active == 0 {
				if g.backup != nil {
					close(g.backup.ready)
				}
				if g.closing {
					close(g.drained)
				}
			}
		})
	}, nil
}

// BeginBackup reserves before waiting. Cancellation before return releases only
// this reservation. Once returned, the caller owns the ticket even if ctx is
// later cancelled and must End it after cleanup; cancellation is not release.
func (g *OperationGate) BeginBackup(ctx context.Context) (*BackupOperation, error) {
	g.mu.Lock()
	if err := ctx.Err(); err != nil {
		g.mu.Unlock()
		return nil, err
	}
	if g.closing {
		g.mu.Unlock()
		return nil, ErrOperationsClosing
	}
	if g.backup != nil {
		g.mu.Unlock()
		return nil, ErrBackupBusy
	}
	op := &BackupOperation{owner: g, ready: make(chan struct{})}
	g.backup = op
	if g.active == 0 {
		close(op.ready)
	}
	g.mu.Unlock()
	select {
	case <-op.ready:
	case <-ctx.Done():
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := ctx.Err(); err != nil {
		g.backup = nil
		return nil, err
	}
	return op, nil
}

func (op *BackupOperation) End() {
	g := op.owner
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.backup == op {
		g.backup = nil
	}
}

// RequestClose rejects waiting/running backups without reserving shutdown.
// check orders existing close decisions: it must not do I/O, wait, or reenter
// this gate. On rejection admission stays open. On acceptance it closes forever;
// wait on the returned normal drain AND any existing drain outside all locks.
// Repeated close decisions are rejected; they never repeat check.
func (g *OperationGate) RequestClose(check func() error) (<-chan struct{}, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closing {
		return nil, ErrOperationsClosing
	}
	if g.backup != nil {
		return nil, ErrBackupBusy
	}
	if check != nil {
		if err := check(); err != nil {
			return nil, err
		}
	}
	g.closing = true
	g.drained = make(chan struct{})
	if g.active == 0 {
		close(g.drained)
	}
	return g.drained, nil
}
