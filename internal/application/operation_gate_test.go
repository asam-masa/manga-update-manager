package application

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func waitForNormalPause(t *testing.T, g *OperationGate) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		end, err := g.BeginNormal()
		if errors.Is(err, ErrOperationsPaused) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		end()
		select {
		case <-timer.C:
			t.Fatal("backup did not reserve admission")
		case <-tick.C:
		}
	}
}

func TestOperationGateBackupDrainsAndOwnsPause(t *testing.T) {
	var g OperationGate
	first, _ := g.BeginNormal()
	second, _ := g.BeginNormal()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan *BackupOperation, 1)
	go func() {
		op, err := g.BeginBackup(ctx)
		if err != nil {
			t.Error(err)
		}
		done <- op
	}()
	waitForNormalPause(t, &g)
	if _, err := g.BeginBackup(ctx); !errors.Is(err, ErrBackupBusy) {
		t.Fatal(err)
	}
	if _, err := g.RequestClose(func() error { t.Error("close check during backup"); return nil }); !errors.Is(err, ErrBackupBusy) {
		t.Fatal(err)
	}
	first()
	first()
	select {
	case <-done:
		t.Fatal("did not wait for second normal operation")
	default:
	}
	second()
	var op *BackupOperation
	select {
	case op = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("did not drain")
	}
	if op == nil {
		t.Fatal("no ticket")
	}
	copy := *op
	copy.End()
	cancel() // Returned ownership is independent of later context cancellation.
	if _, err := g.BeginNormal(); !errors.Is(err, ErrOperationsPaused) {
		t.Fatal(err)
	}
	op.End()
	op.End()
	end, err := g.BeginNormal()
	if err != nil {
		t.Fatal(err)
	}
	end()
	newOp, err := g.BeginBackup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	op.End() // A stale notification cannot release the new reservation.
	if _, err := g.BeginNormal(); !errors.Is(err, ErrOperationsPaused) {
		t.Fatal(err)
	}
	newOp.End()
}

func TestOperationGateCancellationReopensOnlyItsReservation(t *testing.T) {
	for i := 0; i < 20; i++ {
		var g OperationGate
		end, _ := g.BeginNormal()
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() {
			op, err := g.BeginBackup(ctx)
			if op != nil {
				op.End()
				t.Error("cancelled waiter returned ticket")
			}
			result <- err
		}()
		waitForNormalPause(t, &g)
		cancel()
		if i%2 == 0 {
			end()
		} // Both ready and cancellation may be observable.
		select {
		case err := <-result:
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("cancel blocked")
		}
		another, err := g.BeginNormal()
		if err != nil {
			t.Fatal(err)
		}
		another()
		end()
		op, err := g.BeginBackup(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		op.End()
	}
	var g OperationGate
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if op, err := g.BeginBackup(ctx); op != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("early cancel %v %v", op, err)
	}
}

func TestOperationGateCloseDecisionAndConcurrentRelease(t *testing.T) {
	var g OperationGate
	end, _ := g.BeginNormal()
	cause := errors.New("existing close rejected")
	if done, err := g.RequestClose(func() error { return cause }); done != nil || !errors.Is(err, cause) {
		t.Fatal(err)
	}
	other, err := g.BeginNormal()
	if err != nil {
		t.Fatal(err)
	}
	other()
	calls := 0
	done, err := g.RequestClose(func() error { calls++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
		t.Fatal("closed before normal resource release")
	default:
	}
	if _, err := g.BeginNormal(); !errors.Is(err, ErrOperationsClosing) {
		t.Fatal(err)
	}
	if _, err := g.BeginBackup(context.Background()); !errors.Is(err, ErrOperationsClosing) {
		t.Fatal(err)
	}
	if _, err := g.RequestClose(func() error { calls++; return nil }); !errors.Is(err, ErrOperationsClosing) || calls != 1 {
		t.Fatalf("repeated check %d %v", calls, err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); end() }()
	}
	wg.Wait()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("normal release did not complete close drain")
	}
}

func TestOperationGateBackupAndCloseRace(t *testing.T) {
	for i := 0; i < 40; i++ {
		var g OperationGate
		start := make(chan struct{})
		type backupResult struct {
			op  *BackupOperation
			err error
		}
		backups := make(chan backupResult, 1)
		closes := make(chan error, 1)
		go func() { <-start; op, err := g.BeginBackup(context.Background()); backups <- backupResult{op, err} }()
		go func() {
			<-start
			done, err := g.RequestClose(nil)
			if err == nil {
				<-done
			}
			closes <- err
		}()
		close(start)
		var b backupResult
		var closeErr error
		select {
		case b = <-backups:
		case <-time.After(5 * time.Second):
			t.Fatal("backup race blocked")
		}
		select {
		case closeErr = <-closes:
		case <-time.After(5 * time.Second):
			t.Fatal("close race blocked")
		}
		if b.err == nil {
			if b.op == nil || !errors.Is(closeErr, ErrBackupBusy) {
				t.Fatalf("both admitted %v %v", b, closeErr)
			}
			b.op.End()
		} else if !errors.Is(b.err, ErrOperationsClosing) || closeErr != nil {
			t.Fatalf("neither admitted %v %v", b, closeErr)
		}
	}
}
