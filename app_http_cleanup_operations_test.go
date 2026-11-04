package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
)

type startupWaitContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (c *startupWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Done()
}

func TestHTTPCloseBeforeStartupDoesNotWaitForShutdown(t *testing.T) {
	a := NewApp()
	t.Cleanup(func() { a.shutdown(context.Background()) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observed := &startupWaitContext{Context: ctx, entered: make(chan struct{})}
	readDone := make(chan error, 1)
	go func() { _, err := a.readHTTPCover(observed, 1); readDone <- err }()
	select {
	case <-observed.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("startup wait not entered")
	}
	closed := make(chan bool, 1)
	go func() { closed <- a.beforeClose(context.Background()) }()
	select {
	case rejected := <-closed:
		if rejected {
			t.Fatal("close refused before startup")
		}
	case <-time.After(time.Second):
		t.Fatal("close waits for an API which needs startup/shutdown notification")
	}
	a.shutdown(context.Background())
	select {
	case err := <-readDone:
		if err == nil {
			t.Fatal("read after shutdown")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pending read not released")
	}
}

func TestHTTPBackupRefusalAndBufferedSending(t *testing.T) {
	a, _ := coverHTTPFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	sendRelease := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
		select {
		case <-sendRelease:
		default:
			close(sendRelease)
		}
	}()
	calls := 0
	a.coverReader = coverHTTPReaderFunc(func(ctx context.Context, _ int64) ([]byte, error) {
		calls++
		close(entered)
		select {
		case <-release:
			return []byte("snapshot"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	h := a.coverMiddleware(http.NotFoundHandler())
	writer := &blockedCoverWriter{httptest.NewRecorder(), make(chan struct{}), sendRelease}
	sent := make(chan struct{})
	go func() {
		defer close(sent)
		h.ServeHTTP(writer, httptest.NewRequest("GET", "/__covers/works/1", nil).WithContext(ctx))
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
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/__covers/works/1", nil).WithContext(ctx))
	if w.Code != 503 || strings.TrimSpace(w.Body.String()) != "cover unavailable" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("refusal %d %q", w.Code, w.Body.String())
	}
	select {
	case <-ready:
		t.Fatal("backup passed an unfinished read")
	default:
	}
	close(release)
	select {
	case <-writer.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case r := <-ready:
		if r.err != nil || r.op == nil {
			t.Fatalf("backup %#v", r)
		}
		defer r.op.End()
		if calls != 1 {
			t.Fatalf("refused request entered reader: %d", calls)
		}
		if !a.mu.TryLock() {
			t.Fatal("sending retained App lock")
		}
		a.mu.Unlock()
		select {
		case <-sent:
			t.Fatal("fixture did not block sending")
		default:
		}
		close(sendRelease)
		select {
		case <-sent:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		if writer.Body.String() != "snapshot" {
			t.Fatal("buffer changed", writer.Body.String())
		}
		r.op.End()
	case <-ctx.Done():
		t.Fatal("sending blocked backup", ctx.Err())
	}
	a.coverReader = coverHTTPReaderFunc(func(context.Context, int64) ([]byte, error) { return []byte("resumed"), nil })
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/__covers/works/1", nil))
	if w.Code != 200 || w.Body.String() != "resumed" {
		t.Fatalf("resume %d %q", w.Code, w.Body.String())
	}
}

func TestCleanupBackupRefusalKeepsFirstExecution(t *testing.T) {
	a := testApp(t, t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.ctx = ctx
	if err := a.store.ReserveManagedCover(ctx, strings.Repeat("a", 32)); err != nil {
		t.Fatal(err)
	}
	op, err := a.operationGate.BeginBackup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer op.End()
	if _, err := a.LoadCoverWarnings(); errorCode(t, err) != "backup_in_progress" {
		t.Fatal(err)
	}
	if a.coverCleanupPhase.Load() != 0 || len(a.LoadCoverProgress()) != 0 {
		t.Fatal("refused cleanup started")
	}
	op.End()
	warnings, err := a.LoadCoverWarnings()
	if err != nil || len(warnings) != 1 || warnings[0].Code != "cover_cleanup_unverified" {
		t.Fatal("first cleanup consumed by refusal", warnings, err)
	}
	again, err := a.LoadCoverWarnings()
	if err != nil || len(again) != 1 {
		t.Fatal(again, err)
	}
}

func TestBackupWaitsForCleanupAndWarningPublication(t *testing.T) {
	dir := t.TempDir()
	a := testApp(t, dir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.ctx = ctx
	if err := a.store.ReserveManagedCover(ctx, strings.Repeat("b", 32)); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	a.covers = application.NewCoverImporter(cleanupListBlock{a.store, entered, release}, &a.coverOperations, dir, nil, time.Now)
	type result struct {
		warnings []CoverWarningDTO
		err      error
	}
	done := make(chan result, 1)
	go func() { w, err := a.LoadCoverWarnings(); done <- result{w, err} }()
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
	if _, err := a.LoadCoverWarnings(); errorCode(t, err) != "backup_in_progress" {
		t.Fatal(err)
	}
	if len(a.LoadCoverProgress()) != 1 {
		t.Fatal("cleanup progress unavailable")
	}
	select {
	case <-ready:
		t.Fatal("backup passed running cleanup")
	default:
	}
	close(release)
	select {
	case r := <-done:
		if r.err != nil || len(r.warnings) != 1 {
			t.Fatal(r)
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
		if !a.mu.TryLock() {
			t.Fatal("App lock retained")
		}
		a.mu.Unlock()
		if len(a.LoadCoverProgress()) != 0 {
			t.Fatal("progress retained")
		}
		cover, err := a.coverOperations.BeginChange(1, true)
		if err != nil {
			t.Fatal("cleanup ticket retained", err)
		}
		if err := cover.End(); err != nil {
			t.Fatal(err)
		}
		r.op.End()
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	warnings, err := a.LoadCoverWarnings()
	if err != nil || len(warnings) != 1 {
		t.Fatal("cached result lost", warnings, err)
	}
}

func TestClosedAdmissionReturnsCachedWarningsOnly(t *testing.T) {
	a := testApp(t, t.TempDir())
	if err := a.store.ReserveManagedCover(context.Background(), strings.Repeat("c", 32)); err != nil {
		t.Fatal(err)
	}
	warnings, err := a.LoadCoverWarnings()
	if err != nil || len(warnings) != 1 {
		t.Fatal(warnings, err)
	}
	if a.beforeClose(context.Background()) {
		t.Fatal("close refused")
	}
	again, err := a.LoadCoverWarnings()
	if err != nil || len(again) != 1 || again[0] != warnings[0] {
		t.Fatal("cached warning lost", again, err)
	}
	again[0].Message = "caller mutation"
	last, err := a.LoadCoverWarnings()
	if err != nil || last[0] != warnings[0] {
		t.Fatal("cached result aliased", last, err)
	}
	a.shutdown(context.Background())
	if _, err := a.LoadCoverWarnings(); errorCode(t, err) != "database_unavailable" {
		t.Fatal(err)
	}
}
