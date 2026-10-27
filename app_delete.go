package main

import (
	"errors"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

// Values are the exact confirmed snapshot, not editable/normalized new input.
type DeleteWorkInput struct {
	URL           string    `json:"url"`
	Title         string    `json:"title"`
	SiteName      string    `json:"siteName"`
	Notes         string    `json:"notes"`
	ThumbnailPath string    `json:"thumbnailPath"`
	ScheduledDate string    `json:"scheduledDate"`
	Tags          *[]TagDTO `json:"tags"`
}

type DeleteWorkResult struct {
	WorkID   int64            `json:"workID"`
	Warnings []WorkWarningDTO `json:"warnings"`
}

func (a *App) DeleteWork(id int64, expected DeleteWorkInput) (DeleteWorkResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.deleter == nil {
		return DeleteWorkResult{}, unavailableError()
	}
	end, err := a.workOperations.BeginDelete(id)
	if err != nil {
		if errors.Is(err, application.ErrWorkClosing) {
			return DeleteWorkResult{}, unavailableError()
		}
		return DeleteWorkResult{}, &APIError{Code: "work_busy", Message: "この作品は処理中です。処理が終わってから、内容を確認して再度操作してください。"}
	}
	defer end()
	deletion := manga.WorkDeleteInput{URL: expected.URL, Title: expected.Title, SiteName: expected.SiteName, Notes: expected.Notes, ThumbnailPath: expected.ThumbnailPath, ScheduledDate: manga.ScheduledDate(expected.ScheduledDate)}
	if expected.Tags != nil {
		tags, err := tagSnapshots(*expected.Tags)
		if err != nil {
			return DeleteWorkResult{}, invalidTagInput()
		}
		deletion.Tags = &tags
	}
	result, err := a.deleter.DeleteWork(a.ctx, id, deletion)
	if errors.Is(err, storage.ErrWorkDeleteOutcomeUnknown) {
		return DeleteWorkResult{}, &APIError{Code: "work_delete_outcome_unknown", Message: "作品の削除結果を確認できません。新たなデータの読み書きを停止しています。削除を繰り返さず、アプリを再起動して状態を確認してください。"}
	}
	if errors.Is(err, storage.ErrDBStopped) {
		return DeleteWorkResult{}, databaseStoppedError()
	}
	if errors.Is(err, storage.ErrWorkDeleteChanged) {
		if expected.Tags != nil {
			return DeleteWorkResult{}, &APIError{Code: "work_changed", Message: "確認後に作品情報またはタグが変わりました。削除していません。最新の作品情報とタグを読み込み、内容を確認してください。"}
		}
		return DeleteWorkResult{}, &APIError{Code: "work_changed", Message: "確認後に作品情報が変わりました。削除していません。一覧を再読み込みし、最新の作品名とURLを確認してください。"}
	}
	if err != nil {
		return DeleteWorkResult{}, workAPIError(err)
	}
	warnings := make([]WorkWarningDTO, 0, len(result.Warnings))
	for _, code := range result.Warnings {
		message := "作品は削除しましたが、画像の整理が完了しませんでした。画像や管理記録が残っている場合があります。"
		switch code {
		case "database_stopped_after_commit":
			message = "作品は削除しましたが、後始末の安全性を確認できないため新たなデータの読み書きを停止しています。アプリを再起動して状態を確認してください。"
		case "cover_release_unconfirmed":
			message = "作品は削除しましたが、画像の資源解放を確認できないため画像機能を停止しています。アプリを再起動して状態を確認してください。"
		case "cover_unavailable_after_commit":
			message = "作品は削除しましたが、画像機能を利用できないため画像の整理が完了していません。アプリを再起動して状態を確認してください。"
		}
		warnings = append(warnings, WorkWarningDTO{Code: code, Message: message})
	}
	return DeleteWorkResult{WorkID: result.Work.ID, Warnings: warnings}, nil
}
