package application

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
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
		{"unknown", storage.ErrWorkCoverOutcomeUnknown, true},
		{"wrapped-unknown", fmt.Errorf("commit: %w", storage.ErrWorkCoverOutcomeUnknown), true},
		{"cancel-unknown", errors.Join(context.Canceled, storage.ErrWorkCoverOutcomeUnknown), true},
		{"release-unknown", errors.Join(platform.ErrCoverRelease, storage.ErrWorkCoverOutcomeUnknown), true},
		{"same-text-unknown", errors.New(storage.ErrWorkCoverOutcomeUnknown.Error()), false},
		{"input-rejection", storage.ErrInvalidManagedCover, false},
		{"db-stopped", storage.ErrDBStopped, true},
		{"wrapped-db-stopped", fmt.Errorf("read: %w", storage.ErrDBStopped), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var g CoverOperations
			g.ObserveError(tc.err)
			g.ObserveError(nil) // Success must not reset a previously stopped gate.
			for _, begin := range []func() (*CoverOperation, error){g.BeginRead, g.BeginCleanup, func() (*CoverOperation, error) { return g.BeginChange(1, true) }} {
				op, err := begin()
				if tc.stop {
					if op != nil || !errors.Is(err, ErrCoverStopped) {
						t.Fatal("unsafe error did not stop admission")
					}
				} else {
					mustCover(t, op, err).End()
				}
			}
		})
	}
}

func TestCoverObserveErrorPreservesActiveTickets(t *testing.T) {
	for name, failure := range map[string]error{
		"release": errors.Join(context.Canceled, platform.ErrCoverRelease),
		"unknown": errors.Join(context.Canceled, storage.ErrWorkCoverOutcomeUnknown),
		"both":    errors.Join(context.Canceled, platform.ErrCoverRelease, storage.ErrWorkCoverOutcomeUnknown),
	} {
		t.Run(name, func(t *testing.T) { testCoverObservedErrorPreservesActiveTickets(t, failure) })
	}
}

func testCoverObservedErrorPreservesActiveTickets(t *testing.T, failure error) {
	t.Helper()
	var g CoverOperations
	op, err := g.BeginChange(1, false)
	mustCover(t, op, err)
	stage, err := op.BeginStage()
	if err != nil {
		t.Fatal(err)
	}
	read, err := g.BeginRead()
	mustCover(t, read, err)
	wantRelease := errors.Is(failure, platform.ErrCoverRelease)
	wantUnknown := errors.Is(failure, storage.ErrWorkCoverOutcomeUnknown)
	g.ObserveError(failure)
	g.ObserveError(failure)
	if !errors.Is(failure, context.Canceled) ||
		errors.Is(failure, platform.ErrCoverRelease) != wantRelease ||
		errors.Is(failure, storage.ErrWorkCoverOutcomeUnknown) != wantUnknown {
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
	for name, failure := range map[string]error{
		"release": platform.ErrCoverRelease,
		"unknown": storage.ErrWorkCoverOutcomeUnknown,
	} {
		t.Run(name, func(t *testing.T) { testCoverObservedErrorConcurrentStage(t, failure) })
	}
}

func testCoverObservedErrorConcurrentStage(t *testing.T, failure error) {
	t.Helper()
	for range 100 {
		var g CoverOperations
		op, err := g.BeginChange(1, false)
		mustCover(t, op, err)
		start := make(chan struct{})
		var wg sync.WaitGroup
		var stage *CoverStage
		var stageErr error
		wg.Go(func() { <-start; stage, stageErr = op.BeginStage() })
		wg.Go(func() { <-start; g.ObserveError(fmt.Errorf("operation: %w", failure)) })
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
