package application

import (
	"errors"
	"sync"
)

var (
	ErrCoverBusy             = errors.New("cover operation in progress")
	ErrCoverStopped          = errors.New("cover operations disabled")
	ErrCoverClosing          = errors.New("cover operations closing")
	ErrCoverOperationInvalid = errors.New("invalid cover operation")
)

type coverOperationKind uint8

const (
	coverChange coverOperationKind = iota
	coverRead
	coverCleanup
)

// CoverOperations coordinates admission only, not file/DB access or stage locks.
// Its zero value is ready. Do not copy it after first use.
type CoverOperations struct {
	mu        sync.Mutex
	active    map[*CoverOperation]struct{}
	works     map[int64]bool
	selection *CoverOperation
	cleanup   bool
	stopped   bool
	closing   bool
	drained   chan struct{}
}

// CoverOperation must be retained through cleanup and resource release.
// Keep the returned pointer; a copied ticket cannot release it.
type CoverOperation struct {
	owner  *CoverOperations
	kind   coverOperationKind
	workID int64
}

func (g *CoverOperations) BeginChange(workID int64, selecting bool) (*CoverOperation, error) {
	if workID <= 0 {
		return nil, ErrCoverOperationInvalid
	}
	return g.begin(coverChange, workID, selecting)
}

func (g *CoverOperations) BeginRead() (*CoverOperation, error) {
	return g.begin(coverRead, 0, false)
}

// BeginCleanup is exclusive; startup cleanup precedes image admission.
func (g *CoverOperations) BeginCleanup() (*CoverOperation, error) {
	return g.begin(coverCleanup, 0, false)
}

func (g *CoverOperations) begin(kind coverOperationKind, workID int64, selecting bool) (*CoverOperation, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stopped {
		return nil, ErrCoverStopped
	}
	if g.closing {
		return nil, ErrCoverClosing
	}
	if g.cleanup || (kind == coverCleanup && len(g.active) != 0) ||
		(kind == coverChange && (g.works[workID] || (selecting && g.selection != nil))) {
		return nil, ErrCoverBusy
	}
	if g.active == nil {
		g.active = make(map[*CoverOperation]struct{})
		g.works = make(map[int64]bool)
	}
	op := &CoverOperation{owner: g, kind: kind, workID: workID}
	g.active[op] = struct{}{}
	if kind == coverChange {
		g.works[workID] = true
		if selecting {
			g.selection = op
		}
	}
	if kind == coverCleanup {
		g.cleanup = true
	}
	return op, nil
}

// SelectionDone releases only this ticket's dialog slot, not its work slot.
func (op *CoverOperation) SelectionDone() {
	g := op.owner
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.selection == op {
		g.selection = nil
	}
}

// End is idempotent. The caller must finish all resource release attempts first.
// It does not close OS handles or assert that their release succeeded.
func (op *CoverOperation) End() {
	g := op.owner
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.active[op]; !ok {
		return
	}
	delete(g.active, op)
	if op.kind == coverChange {
		delete(g.works, op.workID)
	}
	if g.selection == op {
		g.selection = nil
	}
	if op.kind == coverCleanup {
		g.cleanup = false
	}
	if g.closing && len(g.active) == 0 {
		close(g.drained)
	}
}

// Stop rejects new tickets without cancelling or completing existing tickets.
// Callers must separately stop those operations at safe stage boundaries.
func (g *CoverOperations) Stop() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.stopped = true
}

// RequestClose rejects changes/cleanup without changing admission state. With
// reads only, it permanently closes admission and returns their drain signal.
// Waiting on the signal must not hold the caller's application/DB locks.
func (g *CoverOperations) RequestClose() (<-chan struct{}, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.works) != 0 || g.cleanup {
		return nil, ErrCoverBusy
	}
	if !g.closing {
		g.closing = true
		g.drained = make(chan struct{})
		if len(g.active) == 0 {
			close(g.drained)
		}
	}
	return g.drained, nil
}
