package main

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

func TestReservedBackupDatabaseKeepsCallerAdmission(t *testing.T) {
	for _, mode := range []string{"success", "use-error", "cancel", "sql-release", "cleanup-release", "output-release", "create-unknown"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t, backupAppRoot(t))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			op, _, err := a.beginBackup(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer op.End()
			cause := errors.New("synthetic borrowed work failure")
			cleaned, finalized := false, 0
			assertReserved := func() {
				t.Helper()
				if end, err := a.operationGate.BeginNormal(); err == nil {
					end()
					t.Fatal("borrower resumed normal work")
				} else if !errors.Is(err, application.ErrOperationsPaused) {
					t.Fatal(err)
				}
				if other, err := a.operationGate.ReserveBackup(context.Background()); err == nil {
					other.End()
					t.Fatal("borrower ended caller reservation")
				} else if !errors.Is(err, application.ErrBackupBusy) {
					t.Fatal(err)
				}
			}
			r, released, err := a.runReservedBackupDatabase(ctx, func(ctx context.Context, reader io.ReaderAt, size int64) error {
				assertReserved()
				b := make([]byte, 16)
				if _, err := reader.ReadAt(b, 0); err != nil || size == 0 || string(b) != "SQLite format 3\x00" {
					t.Fatal("invalid copy", err)
				}
				switch mode {
				case "use-error":
					return cause
				case "sql-release":
					return errors.Join(cause, storage.ErrDatabaseCopyRelease)
				case "cancel":
					cancel()
					return ctx.Err()
				}
				return nil
			}, func(ctx context.Context, root string) (application.BackupDatabaseWork, error) {
				assertReserved()
				if mode == "create-unknown" {
					return nil, cause
				}
				work, err := platform.CreateBackupDatabaseWork(ctx, root)
				if err != nil {
					return nil, err
				}
				return &delayedDatabaseWork{BackupDatabaseWork: work, cleanup: func() (platform.BackupDatabaseCleanupResult, error) {
					assertReserved()
					r, err := work.Cleanup()
					cleaned = true
					if mode == "cleanup-release" {
						r.Transition.DataClosed = false // Model release uncertainty after real cleanup.
						err = errors.Join(err, cause)
					}
					return r, err
				}}, nil
			}, func(preparationErr error) (bool, error) {
				assertReserved()
				finalized++
				if mode != "create-unknown" && !cleaned {
					t.Fatal("output finalized before DB cleanup")
				}
				if mode == "output-release" {
					return false, errors.Join(preparationErr, cause)
				}
				return true, preparationErr
			}, nil)
			assertReserved() // Borrower must leave the decision to its caller.
			wantRelease := mode == "success" || mode == "use-error" || mode == "cancel"
			if released != wantRelease || r.FinishError != nil || finalized != 1 {
				t.Fatal("incorrect borrowed outcome", released, r, err)
			}
			if mode == "success" && (err != nil || !r.Database.Consumed) {
				t.Fatal(r, err)
			}
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if mode != "success" && mode != "cancel" && !errors.Is(err, cause) {
				t.Fatal("cause lost", err)
			}
			if mode == "sql-release" && !errors.Is(err, storage.ErrDatabaseCopyRelease) {
				t.Fatal("SQL release lost", err)
			}
			if mode == "create-unknown" && !errors.Is(r.CreationError, cause) {
				t.Fatal(r)
			}
			finishErr := a.finishBackup(op, released)
			if !released {
				if !errors.Is(finishErr, errBackupReleaseUnverified) {
					t.Fatal(finishErr)
				}
				if _, err := a.operationGate.BeginNormal(); !errors.Is(err, application.ErrOperationsStopped) {
					t.Fatal(err)
				}
			} else {
				if finishErr != nil {
					t.Fatal(finishErr)
				}
				end, err := a.operationGate.BeginNormal()
				if err != nil {
					t.Fatal(err)
				}
				end()
			}
		})
	}
}

func TestBackupDatabasePanicDoesNotAssumeRelease(t *testing.T) {
	a := testApp(t, backupAppRoot(t))
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected synthetic panic")
			}
		}()
		_, _ = a.runBackupDatabase(context.Background(), nil, func(context.Context, string) (application.BackupDatabaseWork, error) {
			panic("synthetic creation panic before release observation")
		})
	}()
	if _, err := a.operationGate.BeginNormal(); !errors.Is(err, application.ErrOperationsStopped) {
		t.Fatal("panic resumed admission", err)
	}
}
