package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/backup"
)

func waitBackupAPIResult(t *testing.T, a *App, id string) BackupResultDTO {
	t.Helper()
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if a.GetBackupStatus().Result != nil {
			r, err := a.GetBackupResult(id)
			if err != nil {
				t.Fatal(err)
			}
			return r
		}
		select {
		case <-deadline.C:
			t.Fatal("backup result not retained")
		case <-tick.C:
		}
	}
}

func TestBackupStartAPIRealSave(t *testing.T) {
	for _, mode := range []string{"save", "collision", "relative"} {
		t.Run(mode, func(t *testing.T) {
			a, root := backupArchiveApp(t)
			path := filepath.Join(root, "公開 API 確認.zip")
			if mode == "collision" {
				if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "relative" {
				path = "relative.zip"
			}
			a.chooseBackup = func(context.Context, string) (string, error) { return path, nil }
			got, err := a.SelectAndStartBackup()
			if err != nil || got.Cancelled || got.ExecutionID != "1" {
				t.Fatal(got, err)
			}
			result := waitBackupAPIResult(t, a, got.ExecutionID)
			want := map[string]string{"save": "saved", "collision": "collision", "relative": "not_saved"}[mode]
			if result.Outcome != want {
				t.Fatal(result)
			}
			if mode == "relative" {
				return
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "collision" {
				if string(data) != "keep" {
					t.Fatal("overwritten")
				}
				return
			}
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			m, checkErr := backup.ValidateArchive(f, int64(len(data)))
			closeErr := f.Close()
			if checkErr != nil || closeErr != nil || m.AppVersion != backupBuildVersion() || len(m.Images) != 1 {
				t.Fatal(m, checkErr, closeErr)
			}
		})
	}
}

func TestBackupStartAPICancelAndFailurePreserveResult(t *testing.T) {
	for _, failed := range []bool{false, true} {
		a := testApp(t, t.TempDir())
		run, err := a.startBackupRun("previous.zip", syntheticBackupSuccess)
		if err != nil {
			t.Fatal(err)
		}
		waitBackupRun(t, run.done)
		before := a.backupRuns.snapshot()
		a.chooseBackup = func(context.Context, string) (string, error) {
			if failed {
				return "", errors.New("private-picker-path")
			}
			return "", nil
		}
		got, err := a.SelectAndStartBackup()
		if failed {
			if errorCode(t, err) != "backup_start_failed" || err.Error() == "private-picker-path" {
				t.Fatal(err)
			}
		} else if err != nil || !got.Cancelled {
			t.Fatal(got, err)
		}
		if got.ExecutionID != "" || !reflect.DeepEqual(before, a.backupRuns.snapshot()) {
			t.Fatal("prior result changed")
		}
		op, err := a.operationGate.ReserveBackup(context.Background())
		if err != nil {
			t.Fatal("reservation leaked", err)
		}
		op.End()
	}
}

func TestBackupStartAPISelectionAndNaming(t *testing.T) {
	at := time.Date(2026, 11, 15, 16, 2, 3, 0, time.UTC)
	if got := backupDefaultFilename(at); got != "manga-update-manager-20261116-010203.zip" {
		t.Fatal(got)
	}
	a := testApp(t, t.TempDir())
	a.chooseBackup = func(context.Context, string) (string, error) { t.Error("second picker opened"); return "", nil }
	op, err := a.operationGate.ReserveBackup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SelectAndStartBackup(); errorCode(t, err) != "backup_in_progress" {
		t.Fatal(err)
	}
	op.End()
	_, err = a.fileSelections.choose(context.Background(), func(context.Context) (string, error) {
		if _, err := a.SelectAndStartBackup(); errorCode(t, err) != "file_selection_busy" {
			t.Fatal(err)
		}
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if errorCode(t, backupStartAPIError(application.ErrOperationsStopped)) != "database_stopped" {
		t.Fatal("stop classification")
	}
}
