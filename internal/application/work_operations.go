package application

import (
	"errors"
	"sync"
)

var (
	ErrWorkBusy     = errors.New("work operation in progress")
	ErrWorkClosing  = errors.New("work operations closing")
	ErrWorkDeleting = errors.New("work deletion in progress")
	ErrTagBusy      = errors.New("tag management operation in progress")
)

// WorkOperations coordinates App entry points, not DB/image stage permission.
// Its zero value is ready; do not copy after use.
type WorkOperations struct {
	mu      sync.Mutex
	active  map[int64]struct{}
	tags    map[int64]struct{}
	edits   int
	deletes int
	closing bool
}

func (g *WorkOperations) Begin(id int64) (func(), error)       { return g.begin(id, false, false) }
func (g *WorkOperations) BeginEdit(id int64) (func(), error)   { return g.begin(id, true, false) }
func (g *WorkOperations) BeginDelete(id int64) (func(), error) { return g.begin(id, false, true) }

// BeginTagChange keeps tag and work identities separate, permitting work edits.
// The shared mutex orders tag admission and the final close decision.
func (g *WorkOperations) BeginTagChange(id int64) (func(), error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closing {
		return nil, ErrWorkClosing
	}
	if _, exists := g.tags[id]; exists {
		return nil, ErrTagBusy
	}
	if g.tags == nil {
		g.tags = make(map[int64]struct{})
	}
	g.tags[id] = struct{}{}
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			delete(g.tags, id)
		})
	}, nil
}

func (g *WorkOperations) begin(id int64, editing, deleting bool) (func(), error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closing {
		return nil, ErrWorkClosing
	}
	if _, exists := g.active[id]; exists {
		return nil, ErrWorkBusy
	}
	if g.active == nil {
		g.active = make(map[int64]struct{})
	}
	g.active[id] = struct{}{}
	if editing {
		g.edits++
	}
	if deleting {
		g.deletes++
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
			if deleting {
				g.deletes--
			}
		})
	}, nil
}

// RequestClose orders work/tag admission and the existing image close decision.
// other must only decide admission (no I/O/wait/reentry into this gate).
// Rejection does not close this gate. Wait on the returned channel outside locks.
func (g *WorkOperations) RequestClose(other func() (<-chan struct{}, error)) (<-chan struct{}, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.deletes != 0 {
		return nil, ErrWorkDeleting
	}
	if len(g.tags) != 0 {
		return nil, ErrTagBusy
	}
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
