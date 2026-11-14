package application

import (
	"context"
	"errors"
	"sync"
)

var ErrOperationsPaused = errors.New("normal operations paused for backup")
var ErrOperationsClosing = errors.New("operations closing")
var ErrOperationsStopped = errors.New("operations stopped pending restart")
var ErrBackupBusy = errors.New("backup waiting or running")
var ErrBackupReservationEnded = errors.New("backup reservation ended or not owned")

// OperationGate orders entry admission, not DB/image stage permission. Its zero
// value is ready. Do not copy after use. Every admitted operation must remain
// active through all of its resource releases. Control calls (progress/cancel)
// must not acquire a normal ticket. No I/O or waits run under mu.
type OperationGate struct {
	mu      sync.Mutex
	active  int
	backup  *BackupOperation
	closing bool
	stopped bool
	drained chan struct{}
}

// BackupOperation retains the reservation through all work and resource release.
// Keep the pointer: copied or stale tickets cannot release another reservation.
type BackupOperation struct {
	owner  *OperationGate
	ready  chan struct{}
	ended  chan struct{}
	paused bool // Protected by owner.mu; startup cleanup may enter before pause.
}

func (g *OperationGate) BeginNormal() (func(), error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closing {
		return nil, ErrOperationsClosing
	}
	if g.stopped {
		return nil, ErrOperationsStopped
	}
	if g.backup != nil && g.backup.paused {
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
				if g.backup != nil && g.backup.paused {
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
	op, err := g.ReserveBackup(ctx)
	if err != nil {
		return nil, err
	}
	if err := op.PauseAndWait(ctx); err != nil {
		op.End()
		return nil, err
	}
	return op, nil
}

// ReserveBackup prevents close and duplicate backup entry without pausing normal
// admission. The owner may run/join startup cleanup, then PauseAndWait. Every
// returned ticket must End after all owned cleanup, even if ctx is cancelled.
func (g *OperationGate) ReserveBackup(ctx context.Context) (*BackupOperation, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if g.closing {
		return nil, ErrOperationsClosing
	}
	if g.stopped {
		return nil, ErrOperationsStopped
	}
	if g.backup != nil {
		return nil, ErrBackupBusy
	}
	op := &BackupOperation{owner: g, ready: make(chan struct{}), ended: make(chan struct{})}
	g.backup = op
	return op, nil
}

// PauseAndWait stops new normal entry and drains outside the lock. Cancellation
// returns without releasing the reservation: the owner must finish cleanup and
// End. Repeated waits never create a second drain or release another ticket.
func (op *BackupOperation) PauseAndWait(ctx context.Context) error {
	g := op.owner
	g.mu.Lock()
	if g.backup != op {
		g.mu.Unlock()
		return ErrBackupReservationEnded
	}
	if err := ctx.Err(); err != nil {
		g.mu.Unlock()
		return err
	}
	if !op.paused {
		op.paused = true
		if g.active == 0 {
			close(op.ready)
		}
	}
	g.mu.Unlock()
	select {
	case <-op.ready:
	case <-op.ended:
	case <-ctx.Done():
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.backup != op {
		return ErrBackupReservationEnded
	}
	return ctx.Err()
}

func (op *BackupOperation) End() {
	op.EndWithResume(true)
}

// EndWithResume is called once all cleanup attempts have finished. Resume must
// include both resource release confirmation and the caller's DB/image checks.
// A refusal latches admission stopped before releasing this reservation, but
// still permits RequestClose. A stale/copied ticket cannot change any state.
func (op *BackupOperation) EndWithResume(resume bool) {
	g := op.owner
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.backup == op {
		if !resume {
			g.stopped = true
		}
		g.backup = nil
		close(op.ended)
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
