package main

import (
	"context"
	"errors"
	"sync"
)

var errFileSelectionBusy = errors.New("another file selection is open")

// fileSelectionGate covers native pickers and backup close notices, never file I/O.
// No callback or wait runs under mu. Requests are rejected, not queued.
type fileSelectionGate struct {
	mu   sync.Mutex
	busy bool
}

func (g *fileSelectionGate) choose(ctx context.Context, choose func(context.Context) (string, error)) (string, error) {
	g.mu.Lock()
	if g.busy {
		g.mu.Unlock()
		return "", errFileSelectionBusy
	}
	g.busy = true
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		g.busy = false
		g.mu.Unlock()
	}()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return choose(ctx)
}

func (a *App) selectCoverFile(ctx context.Context) (string, error) {
	return a.fileSelections.choose(ctx, a.chooseCover)
}
