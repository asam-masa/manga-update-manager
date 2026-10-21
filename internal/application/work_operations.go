package application

import (
	"errors"
	"sync"
)

var (
	ErrWorkBusy    = errors.New("work operation in progress")
	ErrWorkClosing = errors.New("work operations closing")
)

// WorkOperations coordinates App entry points, not DB/image stage permission.
// Its zero value is ready; do not copy after use.
type WorkOperations struct {
	mu      sync.Mutex
	active  map[int64]bool // true for an edit, false for URL/image operations
	edits   int
	closing bool
}

func (g *WorkOperations) Begin(id int64) (func(), error)     { return g.begin(id, false) }
func (g *WorkOperations) BeginEdit(id int64) (func(), error) { return g.begin(id, true) }

func (g *WorkOperations) begin(id int64, editing bool) (func(), error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closing {
		return nil, ErrWorkClosing
	}
	if _, exists := g.active[id]; exists {
		return nil, ErrWorkBusy
	}
	if g.active == nil {
		g.active = make(map[int64]bool)
	}
	g.active[id] = editing
	if editing {
		g.edits++
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			delete(g.active, id)
			if editing {
				g.edits--
			}
		})
	}, nil
}

// RequestClose orders edit admission and the existing image close decision.
// other must only decide admission (no I/O/wait/reentry into this gate).
// Rejection does not close this gate. Wait on the returned channel outside locks.
func (g *WorkOperations) RequestClose(other func() (<-chan struct{}, error)) (<-chan struct{}, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.edits != 0 {
		return nil, ErrWorkBusy
	}
	done, err := other()
	if err != nil {
		return nil, err
	}
	g.closing = true
	return done, nil
}
