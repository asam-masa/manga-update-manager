package main

import (
	"errors"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

// The frontend calls this after an initial paint opportunity. Repeated calls join
// one execution and return copies of retained warnings, never retry deletion.
func (a *App) LoadCoverWarnings() ([]CoverWarningDTO, error) {
	<-a.startupFinished // No App lock while waiting for the startup callback.
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.covers == nil {
		return nil, unavailableError()
	}
	a.coverCleanupOnce.Do(func() {
		if !a.coverCleanupPhase.CompareAndSwap(0, 1) {
			return
		}
		if a.coverCleanupTicket == nil {
			return
		}
		codes, err := a.covers.Cleanup(a.ctx, a.coverCleanupTicket)
		for _, code := range codes {
			if code == "cover_cleanup_unverified" {
				a.coverCleanupWarnings = append(a.coverCleanupWarnings, CoverWarningDTO{Code: code, Message: "保存完了を確認できない画像の管理記録を保持しています。安全性を確認できない画像は削除していません。"})
			}
		}
		if err != nil {
			code, message := "cover_cleanup_failed", "画像整理を完了できませんでした。画像や管理記録が残っている場合があります。作品の画像設定は変更していません。"
			if errors.Is(err, storage.ErrDBStopped) {
				code, message = "database_stopped", "画像整理中にデータの安全性を確認できないため、新たな読み書きを停止しています。アプリを再起動して状態を確認してください。"
			} else if errors.Is(err, platform.ErrCoverRelease) || errors.Is(err, platform.ErrUnsafeCoverDirectory) || errors.Is(err, application.ErrCoverStopped) {
				code, message = "cover_unavailable", "画像整理を中断し、画像機能を停止しています。保存先を確認し、アプリを再起動して状態を確認してください。"
			}
			a.coverCleanupWarnings = append(a.coverCleanupWarnings, CoverWarningDTO{Code: code, Message: message})
		}
	})
	result := make([]CoverWarningDTO, len(a.coverCleanupWarnings))
	copy(result, a.coverCleanupWarnings)
	return result, nil
}
