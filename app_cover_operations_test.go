package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
)

func TestCoverAPIsRefuseBackupBeforeSideEffects(t *testing.T) {
	a, dir, _, w := coverStopFixture(t)
	before, err := a.store.LoadLibrarySnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, filepath.FromSlash(w.ThumbnailPath))
	image, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	choices := 0
	a.covers = application.NewCoverImporter(a.store, &a.coverOperations, dir, func(context.Context) (string, error) { choices++; return "", nil }, time.Now)
	a.coverSelectionFinished = func(context.Context, int64, string) { t.Error("unexpected notification") }
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	op, err := a.operationGate.BeginBackup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer op.End()
	for name, call := range map[string]func() error{
		"select":           func() error { _, e := a.SelectWorkCover(w.ID); return e },
		"select-operation": func() error { _, e := a.SelectWorkCoverWithOperation(w.ID, strings.Repeat("e", 32)); return e },
		"remove":           func() error { _, e := a.RemoveWorkCover(w.ID); return e },
		"remove-operation": func() error { _, e := a.RemoveWorkCoverWithOperation(w.ID, strings.Repeat("f", 32)); return e },
	} {
		if err := call(); errorCode(t, err) != "backup_in_progress" {
			t.Fatalf("%s: %v", name, err)
		}
	}
	after, err := a.store.LoadLibrarySnapshot(ctx)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("reference changed", err)
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(image, actual) || choices != 0 || len(a.LoadCoverProgress()) != 0 {
		t.Fatalf("side effects %d %v", choices, err)
	}
	op.End()
	r, err := a.SelectWorkCover(w.ID)
	if err != nil || !r.Cancelled || choices != 1 {
		t.Fatalf("resume %#v %v", r, err)
	}
}

func TestBackupWaitsForCoverWorkAndObservations(t *testing.T) {
	for _, kind := range []string{"select", "select-operation", "remove", "remove-operation", "notification"} {
		t.Run(kind, func(t *testing.T) {
			a, dir, _, w := coverStopFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			a.ctx = ctx
			entered, release := make(chan struct{}), make(chan struct{})
			block := func(ctx context.Context) error {
				close(entered)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			var repo application.CoverRepository = a.store
			if strings.HasPrefix(kind, "remove") {
				repo = blockingWorkLookup{a.store, w.ID, entered, release}
			}
			a.covers = application.NewCoverImporter(repo, &a.coverOperations, dir, func(ctx context.Context) (string, error) {
				if kind == "notification" {
					return "", nil
				}
				return "", block(ctx)
			}, time.Now)
			a.coverSelectionFinished = func(ctx context.Context, _ int64, _ string) {
				if kind == "notification" {
					_ = block(ctx)
				}
			}
			done := make(chan error, 1)
			go func() {
				var err error
				switch kind {
				case "select":
					_, err = a.SelectWorkCover(w.ID)
				case "select-operation", "notification":
					_, err = a.SelectWorkCoverWithOperation(w.ID, strings.Repeat("a", 32))
				case "remove":
					_, err = a.RemoveWorkCover(w.ID)
				case "remove-operation":
					_, err = a.RemoveWorkCoverWithOperation(w.ID, strings.Repeat("b", 32))
				}
				done <- err
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			type outcome struct {
				op  *application.BackupOperation
				err error
			}
			ready := make(chan outcome, 1)
			go func() { op, err := a.operationGate.BeginBackup(ctx); ready <- outcome{op, err} }()
			waitAppGate(t, a, application.ErrOperationsPaused)
			// Progress remains a control call even while normal admission is paused.
			_ = a.LoadCoverProgress()
			select {
			case <-ready:
				t.Fatal("backup passed unfinished image operation")
			default:
			}
			close(release)
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			select {
			case r := <-ready:
				if r.err != nil || r.op == nil {
					t.Fatalf("backup %#v", r)
				}
				defer r.op.End()
				if len(a.LoadCoverProgress()) != 0 {
					t.Fatal("progress record retained")
				}
				if !a.mu.TryLock() {
					t.Fatal("App lock retained")
				}
				a.mu.Unlock()
				end, err := a.workOperations.Begin(w.ID)
				if err != nil {
					t.Fatal("work ticket retained", err)
				}
				end()
				cover, err := a.coverOperations.BeginChange(w.ID, true)
				if err != nil {
					t.Fatal("cover ticket retained", err)
				}
				if err := cover.End(); err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}

func TestCoverEarlyErrorsReleaseAdmission(t *testing.T) {
	a, dir := coverHTTPFixture(t)
	w, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/error", Title: "One"})
	if err != nil {
		t.Fatal(err)
	}
	a.covers = application.NewCoverImporter(a.store, &a.coverOperations, dir, func(context.Context) (string, error) { return "", io.EOF }, time.Now)
	id := strings.Repeat("d", 32)
	_, finish, ok := a.coverProgress.begin(id, w.ID, "select")
	if !ok {
		t.Fatal("fixture")
	}
	defer finish()
	for _, call := range []func() error{
		func() error { _, e := a.SelectWorkCoverWithOperation(w.ID, "bad"); return e },
		func() error { _, e := a.RemoveWorkCoverWithOperation(w.ID, "bad"); return e },
		func() error { _, e := a.SelectWorkCoverWithOperation(w.ID, id); return e },
		func() error { _, e := a.RemoveWorkCoverWithOperation(w.ID, id); return e },
		func() error { _, e := a.SelectWorkCover(w.ID); return e },
	} {
		if err := call(); err == nil {
			t.Fatal("expected error")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		op, err := a.operationGate.BeginBackup(ctx)
		cancel()
		if err != nil {
			t.Fatal("normal ticket leaked", err)
		}
		op.End()
	}
}

func TestCoverAPIsPreserveClosingError(t *testing.T) {
	a := NewApp()
	if _, err := a.operationGate.RequestClose(nil); err != nil {
		t.Fatal(err)
	}
	for _, call := range []func() error{
		func() error { _, e := a.SelectWorkCover(1); return e },
		func() error { _, e := a.SelectWorkCoverWithOperation(1, strings.Repeat("e", 32)); return e },
		func() error { _, e := a.RemoveWorkCover(1); return e },
		func() error { _, e := a.RemoveWorkCoverWithOperation(1, strings.Repeat("f", 32)); return e },
	} {
		if err := call(); errorCode(t, err) != "cover_unavailable" {
			t.Fatal(err)
		}
	}
}
