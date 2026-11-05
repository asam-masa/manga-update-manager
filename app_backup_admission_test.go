package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

func TestBeginBackupRetainsReservation(t *testing.T) {
	a := testApp(t, t.TempDir())
	a.backupBusyNotice = func(context.Context) {}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	op, warnings, err := a.beginBackup(ctx)
	if err != nil || op == nil || len(warnings) != 0 {
		t.Fatal(op, warnings, err)
	}
	defer op.End()
	cancel()
	if _, err := a.operationGate.BeginNormal(); !errors.Is(err, application.ErrOperationsPaused) {
		t.Fatal(err)
	}
	if duplicate, _, err := a.beginBackup(context.Background()); err == nil || duplicate != nil {
		t.Fatal("duplicate admitted", err)
	}
	if !a.beforeClose(context.Background()) {
		t.Fatal("close accepted during backup")
	}
	op.End()
	end, err := a.operationGate.BeginNormal()
	if err != nil {
		t.Fatal(err)
	}
	end()
}

func TestBeginBackupChecksAfterDrain(t *testing.T) {
	for _, state := range []string{"ready", "database stopped", "images stopped", "images active"} {
		t.Run(state, func(t *testing.T) {
			a := testApp(t, t.TempDir())
			if _, err := a.LoadCoverWarnings(); err != nil {
				t.Fatal(err)
			}
			end, err := a.operationGate.BeginNormal()
			if err != nil {
				t.Fatal(err)
			}
			defer end()
			type result struct {
				op  *application.BackupOperation
				err error
			}
			done := make(chan result, 1)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			go func() { op, _, err := a.beginBackup(ctx); done <- result{op, err} }()
			waitAppGate(t, a, application.ErrOperationsPaused)
			select {
			case r := <-done:
				t.Fatal("passed active operation", r)
			default:
			}
			var want error
			switch state {
			case "database stopped":
				if err := a.store.Close(); err != nil {
					t.Fatal(err)
				}
				want = storage.ErrDBStopped
			case "images stopped":
				a.coverOperations.Stop()
				want = application.ErrCoverStopped
			case "images active":
				read, err := a.coverOperations.BeginRead()
				if err != nil {
					t.Fatal(err)
				}
				defer read.End()
				want = application.ErrCoverBusy
			}
			end()
			select {
			case r := <-done:
				if !errors.Is(r.err, want) {
					t.Fatal(r.err, want)
				}
				if want == nil {
					if r.op == nil {
						t.Fatal("missing reservation")
					}
					r.op.End()
				} else if r.op != nil {
					r.op.End()
					t.Fatal("failed reservation returned")
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			resume, err := a.operationGate.BeginNormal()
			if err != nil {
				t.Fatal("reservation leaked", err)
			}
			resume()
		})
	}
}

func TestBeginBackupWaitCancellation(t *testing.T) {
	a := testApp(t, t.TempDir())
	if _, err := a.LoadCoverWarnings(); err != nil {
		t.Fatal(err)
	}
	end, err := a.operationGate.BeginNormal()
	if err != nil {
		t.Fatal(err)
	}
	defer end()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		op, _, err := a.beginBackup(ctx)
		if op != nil {
			op.End()
		}
		done <- err
	}()
	waitAppGate(t, a, application.ErrOperationsPaused)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel blocked")
	}
	resume, err := a.operationGate.BeginNormal()
	if err != nil {
		t.Fatal(err)
	}
	resume()
}

func TestBeginBackupClosing(t *testing.T) {
	a := testApp(t, t.TempDir())
	if _, err := a.LoadCoverWarnings(); err != nil {
		t.Fatal(err)
	}
	if a.beforeClose(context.Background()) {
		t.Fatal("close refused")
	}
	op, _, err := a.beginBackup(context.Background())
	if op != nil {
		op.End()
		t.Fatal("closing backup admitted")
	}
	if !errors.Is(err, application.ErrOperationsClosing) {
		t.Fatal(err)
	}
}
