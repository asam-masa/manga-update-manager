package application

import (
	"errors"
	"sync"
	"testing"
)

func mustCover(t *testing.T, op *CoverOperation, err error) *CoverOperation {
	t.Helper()
	if err != nil || op == nil {
		t.Fatalf("admission: %v", err)
	}
	return op
}

func TestCoverSelectionAndWorkOwnership(t *testing.T) {
	var g CoverOperations
	op, err := g.BeginChange(1, true)
	first := mustCover(t, op, err)
	if op, err := g.BeginChange(1, false); op != nil || !errors.Is(err, ErrCoverBusy) {
		t.Fatal("same work admitted")
	}
	if op, err := g.BeginChange(2, true); op != nil || !errors.Is(err, ErrCoverBusy) {
		t.Fatal("second dialog admitted")
	}
	first.SelectionDone()
	op, err = g.BeginChange(2, true)
	second := mustCover(t, op, err)
	first.SelectionDone() // Must not release the new dialog slot.
	if op, err := g.BeginChange(3, true); op != nil || !errors.Is(err, ErrCoverBusy) {
		t.Fatal("old ticket released dialog")
	}
	if done, err := g.RequestClose(); done != nil || !errors.Is(err, ErrCoverBusy) {
		t.Fatal("closed during change")
	}
	first.End()
	op, err = g.BeginChange(1, false)
	replacement := mustCover(t, op, err) // Rejected close did not reserve shutdown.
	first.End()
	if op, err := g.BeginChange(1, false); op != nil || !errors.Is(err, ErrCoverBusy) {
		t.Fatal("old ticket released new work")
	}
	second.End()
	replacement.End()
	done, err := g.RequestClose()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	default:
		t.Fatal("not drained")
	}
}

func TestCoverReadDrainAndStop(t *testing.T) {
	var g CoverOperations
	op, err := g.BeginRead()
	first := mustCover(t, op, err)
	op, err = g.BeginRead()
	second := mustCover(t, op, err)
	copyTicket := *first
	copyTicket.End()
	done, err := g.RequestClose()
	if err != nil {
		t.Fatal(err)
	}
	if op, err := g.BeginRead(); op != nil || !errors.Is(err, ErrCoverClosing) {
		t.Fatal("read admitted after close")
	}
	if op, err := g.BeginChange(1, false); op != nil || !errors.Is(err, ErrCoverClosing) {
		t.Fatal("change admitted after close")
	}
	first.End()
	first.End()
	select {
	case <-done:
		t.Fatal("drained before last read")
	default:
	}
	g.Stop()
	select {
	case <-done:
		t.Fatal("stop completed active read")
	default:
	}
	second.End()
	select {
	case <-done:
	default:
		t.Fatal("not drained after final read")
	}
	again, err := g.RequestClose()
	if err != nil || again != done {
		t.Fatal("unstable close result")
	}
}

func TestCoverStopAndCleanup(t *testing.T) {
	var g CoverOperations
	if op, err := g.BeginChange(0, true); op != nil || !errors.Is(err, ErrCoverOperationInvalid) {
		t.Fatal("invalid work accepted")
	}
	op, err := g.BeginCleanup()
	cleanup := mustCover(t, op, err)
	if op, err := g.BeginRead(); op != nil || !errors.Is(err, ErrCoverBusy) {
		t.Fatal("read during cleanup")
	}
	if op, err := g.BeginChange(1, false); op != nil || !errors.Is(err, ErrCoverBusy) {
		t.Fatal("change during cleanup")
	}
	if done, err := g.RequestClose(); done != nil || !errors.Is(err, ErrCoverBusy) {
		t.Fatal("close during cleanup")
	}
	cleanup.End()
	op, err = g.BeginChange(1, true)
	change := mustCover(t, op, err)
	if op, err := g.BeginCleanup(); op != nil || !errors.Is(err, ErrCoverBusy) {
		t.Fatal("cleanup during change")
	}
	g.Stop()
	g.Stop()
	for _, begin := range []func() (*CoverOperation, error){g.BeginRead, g.BeginCleanup, func() (*CoverOperation, error) { return g.BeginChange(2, true) }} {
		if op, err := begin(); op != nil || !errors.Is(err, ErrCoverStopped) {
			t.Fatal("admitted after stop")
		}
	}
	if done, err := g.RequestClose(); done != nil || !errors.Is(err, ErrCoverBusy) {
		t.Fatal("stop forgot change")
	}
	change.SelectionDone()
	change.End()
	done, err := g.RequestClose()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	default:
		t.Fatal("stopped gate did not drain")
	}
}

func TestCoverConcurrentAdmissionAndClose(t *testing.T) {
	for range 100 {
		var g CoverOperations
		start := make(chan struct{})
		var wg sync.WaitGroup
		var op *CoverOperation
		var admitted, closeErr error
		var done <-chan struct{}
		wg.Go(func() { <-start; op, admitted = g.BeginRead() })
		wg.Go(func() { <-start; done, closeErr = g.RequestClose() })
		close(start)
		wg.Wait()
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		if admitted == nil {
			select {
			case <-done:
				t.Fatal("close missed admitted read")
			default:
			}
			op.End()
		} else if !errors.Is(admitted, ErrCoverClosing) {
			t.Fatal(admitted)
		}
		select {
		case <-done:
		default:
			t.Fatal("not drained")
		}
	}
}

func TestCoverConcurrentBlockingOperationAndClose(t *testing.T) {
	for _, kind := range []string{"change", "cleanup"} {
		t.Run(kind, func(t *testing.T) {
			for range 100 {
				var g CoverOperations
				start := make(chan struct{})
				var wg sync.WaitGroup
				var op *CoverOperation
				var admitted, closeErr error
				var done <-chan struct{}
				wg.Go(func() {
					<-start
					if kind == "change" {
						op, admitted = g.BeginChange(1, true)
					} else {
						op, admitted = g.BeginCleanup()
					}
				})
				wg.Go(func() { <-start; done, closeErr = g.RequestClose() })
				close(start)
				wg.Wait()
				if admitted == nil {
					if !errors.Is(closeErr, ErrCoverBusy) || done != nil {
						t.Fatal("close missed blocking operation")
					}
					op.End()
					// Busy rejection must not reserve closing.
					op, err := g.BeginRead()
					mustCover(t, op, err).End()
				} else {
					if !errors.Is(admitted, ErrCoverClosing) || closeErr != nil {
						t.Fatalf("unexpected results: %v / %v", admitted, closeErr)
					}
					select {
					case <-done:
					default:
						t.Fatal("empty gate not drained")
					}
				}
			}
		})
	}
}

func TestCoverConcurrentAdmissionAndStop(t *testing.T) {
	for range 100 {
		var g CoverOperations
		start := make(chan struct{})
		var wg sync.WaitGroup
		var op *CoverOperation
		var err error
		wg.Go(func() { <-start; op, err = g.BeginChange(1, true) })
		wg.Go(func() { <-start; g.Stop() })
		close(start)
		wg.Wait()
		if err == nil {
			if done, err := g.RequestClose(); done != nil || !errors.Is(err, ErrCoverBusy) {
				t.Fatal("stop lost admitted operation")
			}
			op.End()
		} else if !errors.Is(err, ErrCoverStopped) {
			t.Fatal(err)
		}
		if op, err := g.BeginRead(); op != nil || !errors.Is(err, ErrCoverStopped) {
			t.Fatal("stop not permanent")
		}
	}
}

func TestCoverConcurrentWorkAndStop(t *testing.T) {
	var g CoverOperations
	start := make(chan struct{})
	results := make(chan *CoverOperation, 20)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { <-start; op, _ := g.BeginChange(1, true); results <- op })
	}
	close(start)
	wg.Wait()
	close(results)
	var winner *CoverOperation
	for op := range results {
		if op != nil {
			if winner != nil {
				t.Fatal("multiple admissions")
			}
			winner = op
		}
	}
	if winner == nil {
		t.Fatal("no admission")
	}
	wg.Go(g.Stop)
	wg.Go(winner.SelectionDone)
	wg.Go(func() { winner.End() })
	wg.Go(func() { winner.End() })
	wg.Wait()
	if op, err := g.BeginChange(2, true); op != nil || !errors.Is(err, ErrCoverStopped) {
		t.Fatal("stop lost")
	}
}
