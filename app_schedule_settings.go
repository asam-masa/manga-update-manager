package main

import (
	"errors"

	"github.com/asam-masa/manga-update-manager/internal/storage"
)

func (a *App) LoadShowFutureSchedules() (bool, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.display == nil {
		return false, futureSchedulesReadError()
	}
	visible, err := a.display.LoadShowFutureSchedules(a.ctx)
	if err != nil {
		a.coverOperations.ObserveError(err)
		if errors.Is(err, storage.ErrDBStopped) {
			return false, databaseStoppedError()
		}
		return false, futureSchedulesReadError()
	}
	return visible, nil
}

func futureSchedulesReadError() error {
	return &APIError{Code: "settings_read_failed", Message: "未来の予定の表示設定を読み込めませんでした。未来の予定を表示して続行します。保存先を確認し、必要に応じてアプリを再起動してください。"}
}

func (a *App) SetShowFutureSchedules(visible bool) error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.display == nil {
		return displaySettingsSaveError()
	}
	if err := a.display.SetShowFutureSchedules(a.ctx, visible); err != nil {
		a.coverOperations.ObserveError(err)
		if errors.Is(err, storage.ErrDBStopped) {
			return databaseStoppedError()
		}
		return displaySettingsSaveError()
	}
	return nil
}
