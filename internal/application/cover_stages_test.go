package application

import (
	"errors"
	"sync"
	"testing"
)

func TestCoverStageLifetime(t *testing.T) {
	var g CoverOperations
	op, err := g.BeginChange(1, false)
	mustCover(t, op, err)
	stage, err := op.BeginStage()
	if err != nil {
		t.Fatal(err)
	}
	if next, err := op.BeginStage(); next != nil || !errors.Is(err, ErrCoverBusy) {
		t.Fatal("overlapping stages")
	}
	if err := op.End(); !errors.Is(err, ErrCoverBusy) {
		t.Fatal("ended running stage")
	}
	if next, err := g.BeginChange(1, false); next != nil || !errors.Is(err, ErrCoverBusy) {
		t.Fatal("work slot released early")
	}
	if done, err := g.RequestClose(); done != nil || !errors.Is(err, ErrCoverBusy) {
		t.Fatal("closed during stage")
	}
	// An admitted stage does not hold the gate mutex during simulated I/O.
	read, err := g.BeginRead()
	mustCover(t, read, err).End()
	stage.End()
	next, err := op.BeginStage()
	if err != nil {
		t.Fatal(err)
	}
	stage.End() // A stale completion cannot clear the next stage.
	copyStage := *next
	copyStage.End()
	if err := op.End(); !errors.Is(err, ErrCoverBusy) {
		t.Fatal("stale/copied stage released next")
	}
	g.Stop()
	if err := op.End(); !errors.Is(err, ErrCoverBusy) {
		t.Fatal("stop completed running stage")
	}
	next.End()
	if stage, err := op.BeginStage(); stage != nil || !errors.Is(err, ErrCoverStopped) {
		t.Fatal("stage admitted after stop")
	}
	if err := op.End(); err != nil {
		t.Fatal(err)
	}
	if err := op.End(); err != nil {
		t.Fatal(err)
	}
	if stage, err := op.BeginStage(); stage != nil || !errors.Is(err, ErrCoverOperationInvalid) {
		t.Fatal("ended operation restarted")
	}
}

func TestCoverStageTicketValidation(t *testing.T) {
	var g CoverOperations
	read, err := g.BeginRead()
	mustCover(t, read, err)
	if stage, err := read.BeginStage(); stage != nil || !errors.Is(err, ErrCoverOperationInvalid) {
		t.Fatal("read started mutation stage")
	}
	read.End()
	op, err := g.BeginCleanup()
	mustCover(t, op, err)
	copyOp := *op
	if stage, err := copyOp.BeginStage(); stage != nil || !errors.Is(err, ErrCoverOperationInvalid) {
		t.Fatal("copy started stage")
	}
	stage, err := op.BeginStage()
	if err != nil {
		t.Fatal(err)
	}
	g.Stop()
	stage.End()
	if err := op.End(); err != nil {
		t.Fatal(err)
	}
	done, err := g.RequestClose()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	default:
		t.Fatal("cleanup still active")
	}
}

func TestCoverStageConcurrentStop(t *testing.T) {
	for range 100 {
		var g CoverOperations
		op, err := g.BeginChange(1, false)
		mustCover(t, op, err)
		start := make(chan struct{})
		var wg sync.WaitGroup
		var stage *CoverStage
		var stageErr error
		wg.Go(func() { <-start; stage, stageErr = op.BeginStage() })
		wg.Go(func() { <-start; g.Stop() })
		close(start)
		wg.Wait()
		if stageErr == nil {
			if err := op.End(); !errors.Is(err, ErrCoverBusy) {
				t.Fatal("stop lost admitted stage")
			}
			stage.End()
		} else if !errors.Is(stageErr, ErrCoverStopped) {
			t.Fatal(stageErr)
		}
		if next, err := op.BeginStage(); next != nil || !errors.Is(err, ErrCoverStopped) {
			t.Fatal("next stage admitted after stop")
		}
		if err := op.End(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCoverStageConcurrentOperationEnd(t *testing.T) {
	for range 100 {
		var g CoverOperations
		op, err := g.BeginChange(1, false)
		mustCover(t, op, err)
		start := make(chan struct{})
		var wg sync.WaitGroup
		var stage *CoverStage
		var stageErr, endErr error
		wg.Go(func() { <-start; stage, stageErr = op.BeginStage() })
		wg.Go(func() { <-start; endErr = op.End() })
		close(start)
		wg.Wait()
		if stageErr == nil {
			if !errors.Is(endErr, ErrCoverBusy) {
				t.Fatal("ended admitted stage")
			}
			stage.End()
			if err := op.End(); err != nil {
				t.Fatal(err)
			}
		} else if !errors.Is(stageErr, ErrCoverOperationInvalid) || endErr != nil {
			t.Fatalf("unexpected results: %v / %v", stageErr, endErr)
		}
	}
}
