package application

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestBackupReservationDefersPauseAndRetainsCancellation(t *testing.T) {
	var g OperationGate
	op, err := g.ReserveBackup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer op.End()
	for range 2 { // Returning to zero before pause must not close the drain.
		end, err := g.BeginNormal()
		if err != nil {
			t.Fatal(err)
		}
		end()
	}
	if _, err := g.RequestClose(func() error { t.Error("close check ran"); return nil }); !errors.Is(err, ErrBackupBusy) {
		t.Fatal(err)
	}
	if _, err := g.BeginBackup(context.Background()); !errors.Is(err, ErrBackupBusy) {
		t.Fatal(err)
	}
	copy := *op
	if err := copy.PauseAndWait(context.Background()); !errors.Is(err, ErrBackupReservationEnded) {
		t.Fatal("copied ticket paused normal entry", err)
	}
	end, err := g.BeginNormal()
	if err != nil {
		t.Fatal(err)
	}
	defer end()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- op.PauseAndWait(ctx) }()
	waitForNormalPause(t, &g)
	select {
	case err := <-done:
		t.Fatal("did not drain normal operation", err)
	default:
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pause did not cancel")
	}
	if _, err := g.BeginNormal(); !errors.Is(err, ErrOperationsPaused) {
		t.Fatal("cancellation released reservation", err)
	}
	if _, err := g.RequestClose(nil); !errors.Is(err, ErrBackupBusy) {
		t.Fatal(err)
	}
	end()
	op.End()
	next, err := g.ReserveBackup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer next.End()
	op.End()
	if err := op.PauseAndWait(context.Background()); !errors.Is(err, ErrBackupReservationEnded) {
		t.Fatal("old ticket paused next run", err)
	}
	if err := next.PauseAndWait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := next.PauseAndWait(context.Background()); err != nil {
		t.Fatal("repeated wait failed", err)
	}
}

func TestBackupReservationEndWakesWaiter(t *testing.T) {
	var g OperationGate
	end, _ := g.BeginNormal()
	defer end()
	op, err := g.ReserveBackup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- op.PauseAndWait(context.Background()) }()
	waitForNormalPause(t, &g)
	op.End()
	select {
	case err := <-done:
		if !errors.Is(err, ErrBackupReservationEnded) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ended reservation left waiter blocked")
	}
}

func TestBackupReservationCloseRace(t *testing.T) {
	for range 40 {
		var g OperationGate
		start := make(chan struct{})
		var op *BackupOperation
		var reserveErr, closeErr error
		var wg sync.WaitGroup
		wg.Go(func() { <-start; op, reserveErr = g.ReserveBackup(context.Background()) })
		wg.Go(func() { <-start; _, closeErr = g.RequestClose(nil) })
		close(start)
		wg.Wait()
		if reserveErr == nil {
			if op == nil || !errors.Is(closeErr, ErrBackupBusy) {
				t.Fatal("close passed reservation", op, closeErr)
			}
			op.End()
		} else if !errors.Is(reserveErr, ErrOperationsClosing) || closeErr != nil || op != nil {
			t.Fatal("inconsistent winner", op, reserveErr, closeErr)
		}
	}
}
