package main

import (
	"errors"

	"github.com/asam-masa/manga-update-manager/internal/settings"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

func (a *App) LoadWorkSort() (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.display == nil {
		return "", workSortReadError()
	}
	order, err := a.display.LoadWorkSort(a.ctx)
	if err != nil {
		a.coverOperations.ObserveError(err)
		if errors.Is(err, storage.ErrDBStopped) {
			return "", databaseStoppedError()
		}
		return "", workSortReadError()
	}
	return string(order), nil
}

func workSortReadError() error {
	return &APIError{Code: "settings_read_failed", Message: "一覧の並び順を読み込めませんでした。登録の古い順で続行します。保存先を確認し、必要に応じてアプリを再起動してください。"}
}

func (a *App) SetWorkSort(order string) error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.display == nil {
		return displaySettingsSaveError()
	}
	if err := a.display.SetWorkSort(a.ctx, settings.WorkSort(order)); err != nil {
		a.coverOperations.ObserveError(err)
		if errors.Is(err, storage.ErrDBStopped) {
			return databaseStoppedError()
		}
		if errors.Is(err, settings.ErrWorkSort) {
			return &APIError{Code: "invalid_input", Message: "一覧の並び順を選択し直してください。"}
		}
		return displaySettingsSaveError()
	}
	return nil
}
