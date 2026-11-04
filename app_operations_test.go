package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
)

func waitAppGate(t *testing.T, a *App, want error) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		end, err := a.operationGate.BeginNormal()
		if errors.Is(err, want) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		end()
		select {
		case <-deadline.C:
			t.Fatal("gate did not change state")
		case <-tick.C:
		}
	}
}

func TestWorkAPIsRefuseBackupBeforeSideEffects(t *testing.T) {
	a := testApp(t, t.TempDir())
	w, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/one", Title: "Before"})
	if err != nil {
		t.Fatal(err)
	}
	opened := 0
	a.opener = application.NewWorkOpener(a.store, func(string) error { opened++; return nil }, time.Now)
	op, err := a.operationGate.BeginBackup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer op.End()
	calls := map[string]func() error{
		"create": func() error {
			_, e := a.CreateWork(CreateWorkInput{URL: "https://example.com/two", Title: "New"})
			return e
		},
		"list":     func() error { _, e := a.ListWorks(); return e },
		"snapshot": func() error { _, e := a.LoadLibrarySnapshot(); return e },
		"open":     func() error { _, e := a.OpenWork(w.ID); return e },
		"edit":     func() error { _, e := a.EditWork(w.ID, EditWorkInput{URL: w.URL, Title: "Changed"}); return e },
		"delete":   func() error { _, e := a.DeleteWork(w.ID, DeleteWorkInput{URL: w.URL, Title: w.Title}); return e },
	}
	for name, call := range calls {
		if err := call(); errorCode(t, err) != "backup_in_progress" || strings.Contains(err.Error(), "manga.sqlite") {
			t.Fatalf("%s: %v", name, err)
		}
	}
	stored, err := a.store.WorkByID(context.Background(), w.ID)
	if err != nil || stored.Title != "Before" || stored.LastAccessedAt != nil || opened != 0 {
		t.Fatalf("side effect: %#v %d %v", stored, opened, err)
	}
	op.End()
	works, err := a.ListWorks()
	if err != nil || len(works) != 1 {
		t.Fatalf("list resumed %#v %v", works, err)
	}
	if _, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/two", Title: "New"}); err != nil {
		t.Fatal(err)
	}
}

type blockedAccessSave struct {
	application.AccessRepository
	entered, release chan struct{}
}

func (r blockedAccessSave) RecordLastAccess(ctx context.Context, id int64, at time.Time) error {
	close(r.entered)
	select {
	case <-r.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return r.AccessRepository.RecordLastAccess(ctx, id, at)
}

func TestBackupWaitsForOpenWorkSaveAndReleases(t *testing.T) {
	for _, mode := range []string{"cancel", "drain"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t, t.TempDir())
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			a.ctx = ctx
			w, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/one", Title: "One"})
			if err != nil {
				t.Fatal(err)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			a.opener = application.NewWorkOpener(blockedAccessSave{a.store, entered, release}, func(string) error { return nil }, time.Now)
			opened := make(chan error, 1)
			go func() { _, err := a.OpenWork(w.ID); opened <- err }()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			backupCtx, stopBackup := context.WithCancel(ctx)
			defer stopBackup()
			type outcome struct {
				op  *application.BackupOperation
				err error
			}
			ready := make(chan outcome, 1)
			go func() { op, err := a.operationGate.BeginBackup(backupCtx); ready <- outcome{op, err} }()
			waitAppGate(t, a, application.ErrOperationsPaused)
			notices := 0
			a.backupBusyNotice = func(context.Context) { notices++ }
			if !a.beforeClose(ctx) || notices != 1 {
				t.Fatal("waiting backup did not prevent close")
			}
			select {
			case <-ready:
				t.Fatal("backup passed unfinished access save")
			default:
			}
			if mode == "cancel" {
				stopBackup()
				select {
				case r := <-ready:
					if r.op != nil || !errors.Is(r.err, context.Canceled) {
						t.Fatalf("cancel %#v", r)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				if _, err := a.ListWorks(); err != nil {
					t.Fatal("normal admission not restored", err)
				}
			}
			close(release)
			select {
			case err := <-opened:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if mode == "drain" {
				select {
				case r := <-ready:
					if r.err != nil || r.op == nil {
						t.Fatalf("drain %#v", r)
					}
					defer r.op.End()
					stored, err := a.store.WorkByID(ctx, w.ID)
					if err != nil || stored.LastAccessedAt == nil {
						t.Fatalf("save not finished %#v %v", stored, err)
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
					if !a.beforeClose(ctx) || notices != 2 {
						t.Fatal("running backup did not prevent close")
					}
					r.op.End()
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			if a.beforeClose(ctx) {
				t.Fatal("close not restored")
			}
		})
	}
}

func TestCloseWaitsForNormalAndImageDrains(t *testing.T) {
	for _, first := range []string{"normal", "image"} {
		t.Run(first, func(t *testing.T) {
			a := NewApp()
			end, _ := a.operationGate.BeginNormal()
			image, err := a.coverOperations.BeginRead()
			if err != nil {
				t.Fatal(err)
			}
			closed := make(chan bool, 1)
			go func() { closed <- a.beforeClose(context.Background()) }()
			waitAppGate(t, a, application.ErrOperationsClosing)
			if !a.beforeClose(context.Background()) {
				t.Fatal("second close stole drain ownership")
			}
			if first == "normal" {
				end()
			} else if err := image.End(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-closed:
				t.Fatal("close passed unfinished drain")
			default:
			}
			end()
			if err := image.End(); err != nil {
				t.Fatal(err)
			}
			select {
			case rejected := <-closed:
				if rejected {
					t.Fatal("close rejected")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("close deadlock")
			}
		})
	}
}
