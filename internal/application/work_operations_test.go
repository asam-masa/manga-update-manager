package application

import (
	"errors"
	"sync"
	"testing"
)

func TestWorkOperationsExclusionAndClose(t *testing.T) {
	var g WorkOperations
	end, err := g.BeginEdit(1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Begin(1); !errors.Is(err, ErrWorkBusy) {
		t.Fatal(err)
	}
	other, err := g.Begin(2)
	if err != nil {
		t.Fatal(err)
	}
	other()
	called := false
	ready := make(chan struct{})
	close(ready)
	decide := func() (<-chan struct{}, error) { called = true; return ready, nil }
	if _, err := g.RequestClose(decide); !errors.Is(err, ErrWorkBusy) || called {
		t.Fatal(err, called)
	}
	end()
	end()
	// An old release cannot remove a newer operation's reservation.
	next, err := g.BeginEdit(1)
	if err != nil {
		t.Fatal(err)
	}
	end()
	if _, err := g.Begin(1); !errors.Is(err, ErrWorkBusy) {
		t.Fatal(err)
	}
	next()
	if _, err := g.RequestClose(func() (<-chan struct{}, error) { return nil, ErrCoverBusy }); !errors.Is(err, ErrCoverBusy) {
		t.Fatal(err)
	}
	again, err := g.Begin(1)
	if err != nil {
		t.Fatal(err)
	}
	again()
	if _, err := g.RequestClose(decide); err != nil || !called {
		t.Fatal(err, called)
	}
	if _, err := g.BeginEdit(1); !errors.Is(err, ErrWorkClosing) {
		t.Fatal(err)
	}
}

func TestWorkOperationsEditVersusClose(t *testing.T) {
	for range 100 {
		var g WorkOperations
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		var editErr, closeErr error
		var end func()
		go func() { defer wg.Done(); <-start; end, editErr = g.BeginEdit(1) }()
		go func() {
			defer wg.Done()
			<-start
			_, closeErr = g.RequestClose(func() (<-chan struct{}, error) { done := make(chan struct{}); close(done); return done, nil })
		}()
		close(start)
		wg.Wait()
		if editErr == nil {
			if !errors.Is(closeErr, ErrWorkBusy) {
				t.Fatal(editErr, closeErr)
			}
			end()
		} else if !errors.Is(editErr, ErrWorkClosing) || closeErr != nil {
			t.Fatal(editErr, closeErr)
		}
	}
}

func TestWorkOperationsDeleteVersusClose(t *testing.T) {
	for range 100 {
		var g WorkOperations
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		var deleteErr, closeErr error
		var end func()
		go func() { defer wg.Done(); <-start; end, deleteErr = g.BeginDelete(1) }()
		go func() {
			defer wg.Done()
			<-start
			_, closeErr = g.RequestClose(func() (<-chan struct{}, error) { ready := make(chan struct{}); close(ready); return ready, nil })
		}()
		close(start)
		wg.Wait()
		if deleteErr == nil {
			if !errors.Is(closeErr, ErrWorkDeleting) {
				t.Fatal(deleteErr, closeErr)
			}
			end()
			end()
		} else if !errors.Is(deleteErr, ErrWorkClosing) || closeErr != nil {
			t.Fatal(deleteErr, closeErr)
		}
	}
}
