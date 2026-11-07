package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
	"golang.org/x/sys/windows"
)

func backupAppRoot(t *testing.T) string {
	t.Helper()
	if err := windows.NewLazySystemDLL("kernel32.dll").NewProc("CreateDirectory2W").Find(); err != nil {
		t.Skipf("positive capability test: %v", err)
	}
	f, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]uint16, 32768)
	n, nameErr := windows.GetFinalPathNameByHandle(windows.Handle(f.Fd()), &buf[0], uint32(len(buf)), 0)
	if err := errors.Join(nameErr, f.Close()); err != nil || n == 0 || n >= uint32(len(buf)) {
		t.Fatal("canonical fixture root", n, err)
	}
	return strings.TrimPrefix(windows.UTF16ToString(buf[:n]), `\\?\`)
}

func TestUseBackupDatabaseLifecycle(t *testing.T) {
	for _, mode := range []string{"success", "use-error", "cancel", "source-stopped", "database-stopped", "retained-child"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t, backupAppRoot(t))
			a.backupBusyNotice = func(context.Context) {}
			a.dataDirectory = func() (string, error) {
				t.Error("resolved directory again after startup")
				return "", errors.New("unexpected resolver")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cause := errors.New("synthetic use failure")
			use := func(_ context.Context, reader io.ReaderAt, size int64) error {
				if size <= 0 {
					t.Fatal("empty DB copy")
				}
				b := make([]byte, 16)
				if _, err := reader.ReadAt(b, 0); err != nil || string(b) != "SQLite format 3\x00" {
					t.Fatal(string(b), err)
				}
				if end, err := a.operationGate.BeginNormal(); end != nil || !errors.Is(err, application.ErrOperationsPaused) {
					t.Fatal("admission reopened during use", err)
				}
				if !a.beforeClose(context.Background()) {
					t.Fatal("exit allowed during use")
				}
				if mode == "cancel" {
					cancel()
					return ctx.Err()
				}
				if mode == "source-stopped" {
					a.coverOperations.Stop()
				}
				if mode == "database-stopped" {
					if err := a.store.Close(); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "use-error" || mode == "source-stopped" || mode == "database-stopped" {
					return cause
				}
				return nil
			}
			var r backupDatabaseRunResult
			var err error
			if mode == "retained-child" {
				r, err = a.runBackupDatabase(ctx, use, func(ctx context.Context, directory string) (application.BackupDatabaseWork, error) {
					w, err := platform.CreateBackupDatabaseWork(ctx, directory)
					if err != nil {
						return nil, err
					}
					if err := os.WriteFile(filepath.Join(filepath.Dir(w.Path()), "unexpected.txt"), []byte("preserve"), 0600); err != nil {
						_, _ = w.Cleanup()
						t.Fatal(err)
					}
					return w, nil
				})
			} else {
				r, err = a.useBackupDatabase(ctx, use)
			}
			if !r.Database.Copied || !r.Database.Validated || !r.Database.Cleanup.ReleaseConfirmed() || r.CreationError != nil {
				t.Fatal(r, err)
			}
			if mode == "success" && (err != nil || !r.Database.Consumed) {
				t.Fatal(r, err)
			}
			if (mode == "use-error" || mode == "source-stopped" || mode == "database-stopped") && (!errors.Is(err, cause) || !errors.Is(r.Database.OperationError, cause)) {
				t.Fatal("lost use cause", r, err)
			}
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal(r, err)
			}
			if mode == "retained-child" {
				if err == nil || r.Database.CleanupError == nil || !r.Database.Consumed {
					t.Fatal(r, err)
				}
				if b, err := os.ReadFile(filepath.Join(r.Database.Cleanup.Directory.Path, "unexpected.txt")); err != nil || string(b) != "preserve" {
					t.Fatal("retained child lost", err)
				}
			}
			if mode == "source-stopped" || mode == "database-stopped" {
				want := application.ErrCoverStopped
				if mode == "database-stopped" {
					want = storage.ErrDBStopped
				}
				if !errors.Is(r.FinishError, want) {
					t.Fatal("source stop lost", r.FinishError)
				}
				if _, err := a.operationGate.BeginNormal(); !errors.Is(err, application.ErrOperationsStopped) {
					t.Fatal(err)
				}
			} else {
				end, err := a.beginNormalOperation()
				if err != nil || r.FinishError != nil {
					t.Fatal("confirmed release did not resume", err, r.FinishError)
				}
				end()
			}
			if a.beforeClose(context.Background()) {
				t.Fatal("finished run blocked exit")
			}
		})
	}
}

type delayedDatabaseWork struct {
	application.BackupDatabaseWork
	cleanup func() (platform.BackupDatabaseCleanupResult, error)
}

func (w *delayedDatabaseWork) Cleanup() (platform.BackupDatabaseCleanupResult, error) {
	return w.cleanup()
}

func TestUseBackupDatabaseWaitsForCleanup(t *testing.T) {
	for _, confirmed := range []bool{true, false} {
		t.Run(map[bool]string{true: "confirmed-warning", false: "unconfirmed-release"}[confirmed], func(t *testing.T) {
			a := testApp(t, backupAppRoot(t))
			a.backupBusyNotice = func(context.Context) {}
			entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var once sync.Once
			t.Cleanup(func() {
				once.Do(func() { close(release) })
				select {
				case <-finished:
				case <-time.After(5 * time.Second):
					t.Error("run did not finish")
				}
			})
			cause := errors.New("synthetic cleanup warning")
			type outcome struct {
				result backupDatabaseRunResult
				err    error
			}
			done := make(chan outcome, 1)
			go func() {
				defer close(finished)
				r, err := a.runBackupDatabase(ctx, func(context.Context, io.ReaderAt, int64) error { return nil }, func(ctx context.Context, directory string) (application.BackupDatabaseWork, error) {
					w, err := platform.CreateBackupDatabaseWork(ctx, directory)
					if err != nil {
						return nil, err
					}
					return &delayedDatabaseWork{BackupDatabaseWork: w, cleanup: func() (platform.BackupDatabaseCleanupResult, error) {
						close(entered)
						<-release
						r, err := w.Cleanup()
						if !confirmed {
							r.Transition.DataClosed = false
						} // Model an unconfirmed Close after real cleanup.
						return r, errors.Join(err, cause)
					}}, nil
				})
				done <- outcome{r, err}
			}()
			select {
			case <-entered:
			case <-finished:
				t.Fatal("run ended before cleanup", <-done)
			case <-time.After(5 * time.Second):
				t.Fatal("cleanup not reached")
			}
			if end, err := a.operationGate.BeginNormal(); end != nil || !errors.Is(err, application.ErrOperationsPaused) {
				t.Fatal("normal admitted during cleanup", err)
			}
			if op, err := a.operationGate.BeginBackup(context.Background()); op != nil || !errors.Is(err, application.ErrBackupBusy) {
				t.Fatal("backup admitted during cleanup", err)
			}
			if !a.beforeClose(context.Background()) {
				t.Fatal("exit admitted during cleanup")
			}
			cancel() // Wait for cleanup despite cancellation; preserve it at the outer boundary.
			once.Do(func() { close(release) })
			var got outcome
			select {
			case got = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("cleanup did not release run")
			}
			if !errors.Is(got.err, cause) || !errors.Is(got.result.Database.CleanupError, cause) || !errors.Is(got.err, context.Canceled) || got.result.Database.OperationError != nil {
				t.Fatal("cleanup cause lost", got)
			}
			end, err := a.operationGate.BeginNormal()
			if confirmed {
				if err != nil || got.result.FinishError != nil {
					t.Fatal("confirmed cleanup did not resume", got, err)
				}
				end()
			} else if !errors.Is(err, application.ErrOperationsStopped) || !errors.Is(got.err, errBackupReleaseUnverified) || !errors.Is(got.result.FinishError, errBackupReleaseUnverified) {
				t.Fatal("unconfirmed release reopened admission", got, err)
			}
			if a.beforeClose(context.Background()) {
				t.Fatal("finished cleanup prevented exit")
			}
		})
	}
}

func TestUseBackupDatabaseCreationAndPreflightFailures(t *testing.T) {
	for _, mode := range []string{"released", "unconfirmed", "unknown", "early-cancel", "startup-warning"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t, t.TempDir())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "early-cancel" {
				cancel()
			}
			if mode == "startup-warning" {
				if _, err := a.LoadCoverWarnings(); err != nil {
					t.Fatal(err)
				}
				a.coverCleanupWarnings = []CoverWarningDTO{{Code: "cover_cleanup_failed", Message: "synthetic warning"}}
			}
			cause := errors.New("synthetic creation failure")
			called := false
			r, err := a.runBackupDatabase(ctx, func(context.Context, io.ReaderAt, int64) error { t.Error("used after failed creation"); return nil }, func(context.Context, string) (application.BackupDatabaseWork, error) {
				called = true
				if end, err := a.operationGate.BeginNormal(); end != nil || !errors.Is(err, application.ErrOperationsPaused) {
					if end != nil {
						end()
					}
					t.Fatal("normal admitted during creation", err)
				}
				if mode == "unknown" {
					return nil, cause
				}
				return nil, &platform.BackupDatabaseWorkError{OutputAcquisitionKnown: true, OutputAcquired: mode == "unconfirmed", DirectoryRelease: platform.BackupWorkDirectoryReleaseResult{CloseAttempted: true, Parents: platform.BackupDirectoryReleaseResult{CloseAttempted: true}}, Err: cause}
			})
			if mode == "early-cancel" || mode == "startup-warning" {
				if called || r.FinishError != nil || r.CreationError != nil {
					t.Fatal("preflight failure acquired work", r, err)
				}
				if mode == "early-cancel" && !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				if mode == "startup-warning" && (len(r.Warnings) != 1 || !errors.Is(err, errBackupCleanupUnverified)) {
					t.Fatal("warning lost", r, err)
				}
			} else if !called || !errors.Is(err, cause) || !errors.Is(r.CreationError, cause) {
				t.Fatal("creation cause lost", r, err)
			}
			end, admissionErr := a.operationGate.BeginNormal()
			if mode == "unconfirmed" || mode == "unknown" {
				if end != nil || !errors.Is(admissionErr, application.ErrOperationsStopped) || !errors.Is(r.FinishError, errBackupReleaseUnverified) {
					t.Fatal(r, admissionErr)
				}
			} else {
				if admissionErr != nil {
					t.Fatal(admissionErr)
				}
				end()
			}
		})
	}
}
