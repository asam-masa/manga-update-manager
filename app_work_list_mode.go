package main

import (
	"errors"

	"github.com/asam-masa/manga-update-manager/internal/settings"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

// Separate reads keep an unknown mode from breaking the existing form preference.
func (a *App) LoadWorkListMode() (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.display == nil {
		return "", workListModeReadError()
	}
	mode, err := a.display.LoadWorkListMode(a.ctx)
	if err != nil {
		a.coverOperations.ObserveError(err)
		if errors.Is(err, storage.ErrDBStopped) {
			return "", databaseStoppedError()
		}
		return "", workListModeReadError()
	}
	return string(mode), nil
}

func workListModeReadError() error {
	return &APIError{Code: "settings_read_failed", Message: "一覧の表示形式を読み込めませんでした。メモなしカードで続行します。保存先を確認し、必要に応じてアプリを再起動してください。"}
}

func (a *App) SetWorkListMode(mode string) error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.display == nil {
		return displaySettingsSaveError()
	}
	if err := a.display.SetWorkListMode(a.ctx, settings.WorkListMode(mode)); err != nil {
		a.coverOperations.ObserveError(err)
		if errors.Is(err, storage.ErrDBStopped) {
			return databaseStoppedError()
		}
		if errors.Is(err, settings.ErrWorkListMode) {
			return &APIError{Code: "invalid_input", Message: "一覧の表示形式を選択し直してください。"}
		}
		return displaySettingsSaveError()
	}
	return nil
}
