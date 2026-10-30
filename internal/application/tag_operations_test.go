package application

import (
	"errors"
	"sync"
	"testing"
)

func TestTagAdmissionVersusClose(t *testing.T) {
	for range 100 {
		var g WorkOperations
		start := make(chan struct{})
		var wg sync.WaitGroup
		var admitted, closed error
		var end func()
		wg.Add(2)
		go func() { defer wg.Done(); <-start; end, admitted = g.BeginTagChange(1) }()
		go func() {
			defer wg.Done()
			<-start
			_, closed = g.RequestClose(func() (<-chan struct{}, error) { done := make(chan struct{}); close(done); return done, nil })
		}()
		close(start)
		wg.Wait()
		if admitted == nil {
			if !errors.Is(closed, ErrTagBusy) {
				t.Fatal(admitted, closed)
			}
			end()
			end()
			next, err := g.BeginTagChange(1)
			if err != nil {
				t.Fatal(err)
			}
			end() // A stale release must not remove the new reservation.
			if _, err := g.BeginTagChange(1); !errors.Is(err, ErrTagBusy) {
				t.Fatal(err)
			}
			next()
		} else if !errors.Is(admitted, ErrWorkClosing) || closed != nil {
			t.Fatal(admitted, closed)
		}
	}
}
