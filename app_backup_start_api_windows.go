package main

import (
	"context"
	"errors"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type BackupStartDTO struct {
	Cancelled   bool   `json:"cancelled"`
	ExecutionID string `json:"executionId"`
}

// Selection and acceptance are synchronous; the owned save runs independently.
// The frontend never supplies a destination or an application-lifetime context.
func (a *App) SelectAndStartBackup() (BackupStartDTO, error) {
	choose := a.chooseBackup
	if choose == nil {
		choose = chooseBackupDestination
	}
	return a.selectAndStartBackup(choose, time.Now(), backupBuildVersion())
}

func (a *App) selectAndStartBackup(choose func(context.Context, string) (string, error), at time.Time, version string) (BackupStartDTO, error) {
	var destination string
	run, err := a.selectBackupRun(func(ctx context.Context) (string, error) {
		path, err := choose(ctx, backupDefaultFilename(at))
		destination = path // Written before the worker goroutine is started; never changed afterwards.
		return path, err
	}, func(ctx context.Context, id application.BackupExecutionID) (backupSaveRunResult, bool, error) {
		return a.backupSaveWorker(destination, at, version)(ctx, id)
	})
	if err != nil {
		return BackupStartDTO{}, backupStartAPIError(err)
	}
	if run.id == 0 {
		return BackupStartDTO{Cancelled: true}, nil
	}
	return BackupStartDTO{ExecutionID: strconv.FormatUint(uint64(run.id), 10)}, nil
}

func backupDefaultFilename(at time.Time) string {
	return "manga-update-manager-" + platform.InJapan(at).Format("20060102-150405") + ".zip"
}

func chooseBackupDestination(ctx context.Context, name string) (string, error) {
	return runtime.SaveFileDialog(ctx, runtime.SaveDialogOptions{
		Title: "バックアップの保存先（既存ファイルには保存できません）", DefaultFilename: name,
		Filters: []runtime.FileFilter{{DisplayName: "バックアップ（ZIP）", Pattern: "*.zip"}},
	})
}

func backupBuildVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "development"
}

func backupStartAPIError(err error) error {
	var api *APIError
	if errors.As(err, &api) {
		return api
	}
	switch {
	case errors.Is(err, errFileSelectionBusy):
		return &APIError{Code: "file_selection_busy", Message: "別の選択画面を閉じてから、もう一度操作してください。"}
	case errors.Is(err, application.ErrBackupBusy):
		return &APIError{Code: "backup_in_progress", Message: "バックアップの保存先選択中または処理中です。結果を確認してから再度操作してください。"}
	case errors.Is(err, errBackupRetryUnsafe):
		return &APIError{Code: "backup_retry_blocked", Message: "前回の保存結果を確認できません。候補を保全し、再保存せず状態を確認してください。"}
	case errors.Is(err, application.ErrOperationsStopped), errors.Is(err, storage.ErrDBStopped):
		return databaseStoppedError()
	case errors.Is(err, application.ErrCoverStopped), errors.Is(err, application.ErrCoverClosing):
		return &APIError{Code: "cover_unavailable", Message: "画像機能を利用できません。保存先を確認し、アプリを再起動して状態を確認してください。"}
	case errors.Is(err, context.Canceled), errors.Is(err, errBackupRunsClosed):
		return unavailableError()
	default:
		return &APIError{Code: "backup_start_failed", Message: "バックアップを開始できませんでした。保存先選択を閉じ、現在の状況を確認してから再度操作してください。"}
	}
}
