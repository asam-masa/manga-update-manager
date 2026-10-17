package application

import (
	"errors"
	"sync"

	"github.com/asam-masa/manga-update-manager/internal/platform"
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

// CoverOperations coordinates admission and stage starts, not file/DB access.
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
	stage  *CoverStage
}

// CoverStage marks an admitted stage, not successful I/O. Keep its pointer.
type CoverStage struct {
	operation *CoverOperation
}

// BeginStage atomically orders a change/cleanup stage start against Stop.
// No mutex remains held during the caller's actual I/O.
func (op *CoverOperation) BeginStage() (*CoverStage, error) {
	g := op.owner
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.active[op]; !ok || op.kind == coverRead {
		return nil, ErrCoverOperationInvalid
	}
	if g.stopped {
		return nil, ErrCoverStopped
	}
	if op.stage != nil {
		return nil, ErrCoverBusy
	}
	stage := &CoverStage{operation: op}
	op.stage = stage
	return stage, nil
}

// End marks stage completion only. It does not end the overall operation.
func (stage *CoverStage) End() {
	op := stage.operation
	g := op.owner
	g.mu.Lock()
	defer g.mu.Unlock()
	if op.stage == stage {
		op.stage = nil
	}
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
// A running stage returns ErrCoverBusy; finish it and retry operation End.
func (op *CoverOperation) End() error {
	g := op.owner
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.active[op]; !ok {
		return nil
	}
	if op.stage != nil {
		return ErrCoverBusy
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
	return nil
}

// Stop rejects new tickets without cancelling or completing existing tickets.
// Callers must separately stop those operations at safe stage boundaries.
func (g *CoverOperations) Stop() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.stopped = true
}

// ObserveError connects the platform error contract to admission stopping.
// Report all operation/release errors before ending tickets or starting another
// stage. It neither consumes the error nor changes any operation's result.
func (g *CoverOperations) ObserveError(err error) {
	if errors.Is(err, platform.ErrCoverRelease) {
		g.Stop()
	}
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
