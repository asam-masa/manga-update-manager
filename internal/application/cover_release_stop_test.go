package application

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/platform"
)

func TestCoverObserveErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		stop bool
	}{
		{"success", nil, false},
		{"ordinary", errors.New("write failed"), false},
		{"cancel", context.Canceled, false},
		{"same-text", errors.New(platform.ErrCoverRelease.Error()), false},
		{"release", platform.ErrCoverRelease, true},
		{"wrapped", fmt.Errorf("close: %w", platform.ErrCoverRelease), true},
		{"cancel-release", errors.Join(context.Canceled, platform.ErrCoverRelease), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var g CoverOperations
			g.ObserveError(tc.err)
			g.ObserveError(nil) // Success must not reset a previously stopped gate.
			for _, begin := range []func() (*CoverOperation, error){g.BeginRead, g.BeginCleanup, func() (*CoverOperation, error) { return g.BeginChange(1, true) }} {
				op, err := begin()
				if tc.stop {
					if op != nil || !errors.Is(err, ErrCoverStopped) {
						t.Fatal("release failure did not stop admission")
					}
				} else {
					mustCover(t, op, err).End()
				}
			}
		})
	}
}

func TestCoverObserveErrorPreservesActiveTickets(t *testing.T) {
	var g CoverOperations
	op, err := g.BeginChange(1, false)
	mustCover(t, op, err)
	stage, err := op.BeginStage()
	if err != nil {
		t.Fatal(err)
	}
	read, err := g.BeginRead()
	mustCover(t, read, err)
	failure := errors.Join(context.Canceled, platform.ErrCoverRelease)
	g.ObserveError(failure)
	g.ObserveError(failure)
	if !errors.Is(failure, context.Canceled) || !errors.Is(failure, platform.ErrCoverRelease) {
		t.Fatal("lost original error")
	}
	if done, err := g.RequestClose(); done != nil || !errors.Is(err, ErrCoverBusy) {
		t.Fatal("stop completed active operation")
	}
	if err := op.End(); !errors.Is(err, ErrCoverBusy) {
		t.Fatal("stop completed active stage")
	}
	stage.End()
	if next, err := op.BeginStage(); next != nil || !errors.Is(err, ErrCoverStopped) {
		t.Fatal("next stage admitted")
	}
	if err := op.End(); err != nil {
		t.Fatal(err)
	}
	done, err := g.RequestClose()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
		t.Fatal("stop completed read")
	default:
	}
	read.End()
	select {
	case <-done:
	default:
		t.Fatal("read not drained")
	}
}

func TestCoverObserveErrorConcurrentStage(t *testing.T) {
	for range 100 {
		var g CoverOperations
		op, err := g.BeginChange(1, false)
		mustCover(t, op, err)
		start := make(chan struct{})
		var wg sync.WaitGroup
		var stage *CoverStage
		var stageErr error
		wg.Go(func() { <-start; stage, stageErr = op.BeginStage() })
		wg.Go(func() { <-start; g.ObserveError(fmt.Errorf("close: %w", platform.ErrCoverRelease)) })
		close(start)
		wg.Wait()
		if stageErr == nil {
			if err := op.End(); !errors.Is(err, ErrCoverBusy) {
				t.Fatal("lost running stage")
			}
			stage.End()
		} else if !errors.Is(stageErr, ErrCoverStopped) {
			t.Fatal(stageErr)
		}
		if next, err := op.BeginStage(); next != nil || !errors.Is(err, ErrCoverStopped) {
			t.Fatal("subsequent stage admitted")
		}
		if err := op.End(); err != nil {
			t.Fatal(err)
		}
	}
}
