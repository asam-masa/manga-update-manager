package application

import (
	"context"
	"sync"
)

// LockReferences serializes managed-record confirmation, reference changes and
// future cleanup. All application paths that can add image references must use
// this same gate. It does not authorize SQL, file deletion, or operation stages.
// Image Stop alone must not prevent ordinary work registration using this gate.
func (g *CoverOperations) LockReferences(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.mu.Lock()
	if g.references == nil {
		g.references = make(chan struct{}, 1)
	}
	slot := g.references
	g.mu.Unlock()
	select {
	case slot <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		<-slot
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(func() { <-slot }) }, nil
}
