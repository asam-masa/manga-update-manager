package application

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCoverReferencesSerializationAndRelease(t *testing.T) {
	var gate CoverOperations
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var active atomic.Int32
	var workers sync.WaitGroup
	for range 16 {
		workers.Go(func() {
			for range 50 {
				unlock, err := gate.LockReferences(ctx)
				if err != nil {
					t.Error(err)
					return
				}
				if active.Add(1) != 1 {
					t.Error("overlapping reference sections")
				}
				active.Add(-1)
				unlock()
				unlock() // A repeated release must not release another holder.
			}
		})
	}
	workers.Wait()
}

func TestCoverReferencesCancellationAndImageStop(t *testing.T) {
	var gate CoverOperations
	unlock, err := gate.LockReferences(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if release, err := gate.LockReferences(ctx); !errors.Is(err, context.Canceled) || release != nil {
		t.Fatal("cancelled request acquired references", err)
	}
	unlock()
	gate.Stop()
	// Registration still uses the gate after image-only stopping. It does not
	// grant image stage permission and does not bypass Store admission.
	unlock, err = gate.LockReferences(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	if _, err := gate.BeginChange(1, true); !errors.Is(err, ErrCoverStopped) {
		t.Fatal(err)
	}
}
