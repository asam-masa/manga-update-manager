package application

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestOperationGateStopPersistsButAllowsClose(t *testing.T) {
	var g OperationGate
	op, err := g.BeginBackup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	copy := *op
	copy.EndWithResume(false)
	if _, err := g.BeginNormal(); !errors.Is(err, ErrOperationsPaused) {
		t.Fatal("copy changed admission", err)
	}
	op.EndWithResume(false)
	op.End()
	op.EndWithResume(true)
	if _, err := g.BeginNormal(); !errors.Is(err, ErrOperationsStopped) {
		t.Fatal("stop cleared", err)
	}
	if next, err := g.BeginBackup(context.Background()); next != nil || !errors.Is(err, ErrOperationsStopped) {
		t.Fatal(next, err)
	}
	done, err := g.RequestClose(nil)
	if err != nil {
		t.Fatal("stopped gate blocked exit", err)
	}
	select {
	case <-done:
	default:
		t.Fatal("exit drain not complete")
	}
}

func TestOperationGateStaleStopCannotAffectAnotherBackup(t *testing.T) {
	var g OperationGate
	old, _ := g.BeginBackup(context.Background())
	old.EndWithResume(true)
	op, err := g.BeginBackup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	old.EndWithResume(false)
	if _, err := g.BeginNormal(); !errors.Is(err, ErrOperationsPaused) {
		t.Fatal("stale stop changed new backup", err)
	}
	op.EndWithResume(true)
	end, err := g.BeginNormal()
	if err != nil {
		t.Fatal("stale stop latched", err)
	}
	end()
}

func TestOperationGateStopAndAdmissionRace(t *testing.T) {
	for i := 0; i < 40; i++ {
		var g OperationGate
		op, _ := g.BeginBackup(context.Background())
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(3)
		go func() { defer wg.Done(); <-start; op.EndWithResume(false) }()
		go func() {
			defer wg.Done()
			<-start
			end, err := g.BeginNormal()
			if end != nil {
				end()
				t.Error("normal admitted across stop")
			}
			if !errors.Is(err, ErrOperationsPaused) && !errors.Is(err, ErrOperationsStopped) {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			next, err := g.BeginBackup(context.Background())
			if next != nil {
				next.End()
				t.Error("backup admitted across stop")
			}
			if !errors.Is(err, ErrBackupBusy) && !errors.Is(err, ErrOperationsStopped) {
				t.Error(err)
			}
		}()
		close(start)
		wg.Wait()
		if _, err := g.RequestClose(nil); err != nil {
			t.Fatal("stop retained backup reservation", err)
		}
	}
}
