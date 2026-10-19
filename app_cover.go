package main

import (
	"context"
	"errors"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
	"github.com/asam-masa/manga-update-manager/internal/thumbnail"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type CoverWarningDTO struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type SelectWorkCoverResult struct {
	Cancelled bool              `json:"cancelled"`
	Work      *WorkDTO          `json:"work"`
	Warnings  []CoverWarningDTO `json:"warnings"`
}

type RemoveWorkCoverResult struct {
	Work     WorkDTO           `json:"work"`
	Warnings []CoverWarningDTO `json:"warnings"`
}

func (a *App) RemoveWorkCover(workID int64) (RemoveWorkCoverResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.covers == nil {
		return RemoveWorkCoverResult{}, unavailableError()
	}
	result, err := a.covers.Remove(a.ctx, workID)
	if err != nil {
		apiErr := coverAPIError(err)
		if converted, ok := apiErr.(*APIError); ok && converted.Code == "cover_import_failed" {
			apiErr = &APIError{Code: "cover_remove_failed", Message: "画像を解除できませんでした。保存先を確認してから再度操作してください。"}
		}
		return RemoveWorkCoverResult{}, apiErr
	}
	return RemoveWorkCoverResult{Work: workDTO(*result.Work), Warnings: coverWarnings(result.Warnings, "画像を解除しましたが、")}, nil
}

func chooseCover(ctx context.Context) (string, error) {
	return runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{Title: "作品の画像を選択", Filters: []runtime.FileFilter{{DisplayName: "画像（JPEG・PNG・WebP）", Pattern: "*.jpg;*.jpeg;*.png;*.webp"}}})
}

func (a *App) SelectWorkCover(workID int64) (SelectWorkCoverResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.covers == nil {
		return SelectWorkCoverResult{}, unavailableError()
	}
	result, err := a.covers.Select(a.ctx, workID)
	if err != nil {
		return SelectWorkCoverResult{}, coverAPIError(err)
	}
	dto := SelectWorkCoverResult{Cancelled: result.Cancelled, Warnings: make([]CoverWarningDTO, 0, len(result.Warnings))}
	if result.Work != nil {
		work := workDTO(*result.Work)
		dto.Work = &work
	}
	dto.Warnings = coverWarnings(result.Warnings, "画像を設定しましたが、")
	return dto, nil
}

func coverWarnings(codes []string, prefix string) []CoverWarningDTO {
	warnings := make([]CoverWarningDTO, 0, len(codes))
	for _, code := range codes {
		message := prefix + "旧画像の後処理を確認できませんでした。確定済みの設定は維持しています。旧画像や管理記録が残っている場合があります。"
		if code == "cover_unavailable_after_commit" {
			message = prefix + "画像機能を停止しています。アプリを再起動して状態を確認してください。"
		}
		if code == "cover_release_unconfirmed" {
			message = prefix + "資源の解放を確認できないため画像機能を停止しています。アプリを再起動して状態を確認してください。"
		}
		if code == "database_stopped_after_commit" {
			message = prefix + "後始末を確認できないため新たな読み書きを停止しています。アプリを再起動して状態を確認してください。"
		}
		warnings = append(warnings, CoverWarningDTO{Code: code, Message: message})
	}
	return warnings
}

func coverAPIError(err error) error {
	if errors.Is(err, storage.ErrWorkCoverOutcomeUnknown) {
		return &APIError{Code: "cover_outcome_unknown", Message: "画像の変更結果を確認できませんでした。画像は削除せず保持しています。アプリを再起動して状態を確認してください。"}
	}
	if errors.Is(err, storage.ErrDBStopped) {
		return databaseStoppedError()
	}
	if errors.Is(err, application.ErrCoverBusy) {
		return &APIError{Code: "cover_busy", Message: "画像の選択または処理中です。完了後に再度操作してください。"}
	}
	if errors.Is(err, application.ErrCoverStopped) || errors.Is(err, application.ErrCoverClosing) || errors.Is(err, platform.ErrCoverRelease) || errors.Is(err, platform.ErrUnsafeCoverDirectory) {
		return &APIError{Code: "cover_unavailable", Message: "画像機能を利用できません。保存先を確認し、アプリを再起動して状態を確認してください。"}
	}
	for _, inputErr := range []error{thumbnail.ErrTooLarge, thumbnail.ErrDimensions, thumbnail.ErrUnsupported, thumbnail.ErrAnimation, thumbnail.ErrInvalid, thumbnail.ErrOrientation} {
		if errors.Is(err, inputErr) {
			return &APIError{Code: "invalid_cover", Message: inputErr.Error()}
		}
	}
	if errors.Is(err, application.ErrCoverOperationInvalid) {
		return &APIError{Code: "invalid_input", Message: "作品IDを確認してください。"}
	}
	if errors.Is(err, storage.ErrWorkNotFound) {
		return workAPIError(err)
	}
	return &APIError{Code: "cover_import_failed", Message: "画像を取り込めませんでした。未完了の画像や管理記録が残っている場合があります。保存先を確認してください。"}
}

func (a *App) beforeClose(ctx context.Context) bool {
	a.mu.RLock()
	// No cleanup has begun yet: closing can discard the pending startup ticket.
	// Once claimed, normal close must not cancel or wait out the running cleanup.
	if a.coverCleanupTicket != nil && a.coverCleanupPhase.CompareAndSwap(0, 2) {
		_ = a.coverCleanupTicket.End()
	}
	done, err := a.coverOperations.RequestClose()
	a.mu.RUnlock()
	if err != nil {
		a.coverBusyNotice(ctx)
		return true
	}
	<-done
	return false
}

func coverBusyNotice(ctx context.Context) {
	_, _ = runtime.MessageDialog(ctx, runtime.MessageDialogOptions{Type: runtime.InfoDialog, Title: "画像を処理中です", Message: "画像の処理中は終了できません。処理結果を確認してから、もう一度閉じてください。"})
}
