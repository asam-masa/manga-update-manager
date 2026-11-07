package main

import (
	"context"
	"errors"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

func TestFinishBackupAdmissionAndClose(t *testing.T) {
	for _, state := range []string{"released", "unverified", "database stopped", "images stopped", "multiple"} {
		t.Run(state, func(t *testing.T) {
			a := testApp(t, t.TempDir())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			op, _, err := a.beginBackup(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer op.End()
			cancel() // Cancellation does not determine resource release or admission.
			if state == "database stopped" || state == "multiple" {
				if err := a.store.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if state == "images stopped" {
				a.coverOperations.Stop()
			}
			r := platform.BackupDatabaseCleanupResult{
				Transition:       platform.BackupDatabaseTransitionResult{DataClosed: true, CleanupHandleAcquisitionKnown: true},
				DirectoryRelease: platform.BackupWorkDirectoryReleaseResult{CloseAttempted: true, Parents: platform.BackupDirectoryReleaseResult{CloseAttempted: true}},
			}
			if state == "unverified" || state == "multiple" {
				r = platform.BackupDatabaseCleanupResult{}
			}
			err = a.finishBackup(op, r.ReleaseConfirmed())
			if state == "released" {
				if err != nil {
					t.Fatal(err)
				}
				end, err := a.beginNormalOperation()
				if err != nil {
					t.Fatal("confirmed release did not resume", err)
				}
				end()
				next, _, err := a.beginBackup(context.Background())
				if err != nil {
					t.Fatal("backup did not resume", err)
				}
				next.End()
			} else {
				if err == nil {
					t.Fatal("unsafe resume accepted")
				}
				if (state == "unverified" || state == "multiple") && !errors.Is(err, errBackupReleaseUnverified) {
					t.Fatal(err)
				}
				if (state == "database stopped" || state == "multiple") && !errors.Is(err, storage.ErrDBStopped) {
					t.Fatal(err)
				}
				if state == "images stopped" && !errors.Is(err, application.ErrCoverStopped) {
					t.Fatal(err)
				}
				if _, err := a.operationGate.BeginNormal(); !errors.Is(err, application.ErrOperationsStopped) {
					t.Fatal("normal not stopped", err)
				}
				if next, err := a.operationGate.BeginBackup(context.Background()); next != nil || !errors.Is(err, application.ErrOperationsStopped) {
					t.Fatal(next, err)
				}
				_, apiErr := a.beginNormalOperation()
				var detail *APIError
				if !errors.As(apiErr, &detail) || detail.Code != "database_stopped" {
					t.Fatal("missing restart guidance", apiErr)
				}
			}
			if a.beforeClose(context.Background()) {
				t.Fatal("finished backup prevented normal exit")
			}
		})
	}
}
