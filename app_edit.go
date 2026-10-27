package main

import (
	"errors"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

type EditWorkInput struct {
	URL      string `json:"url"`
	Title    string `json:"title"`
	SiteName string `json:"siteName"`
	Notes    string `json:"notes"`
	// nil (including JSON null) preserves the date; an explicit empty string clears it.
	ScheduledDate *string `json:"scheduledDate"`
	// nil (including JSON null) preserves tags; an empty object clears them.
	Tags *TagEditInput `json:"tags"`
}

type WorkWarningDTO struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type EditWorkResult struct {
	Work     WorkDTO          `json:"work"`
	Warnings []WorkWarningDTO `json:"warnings"`
}

func (a *App) EditWork(id int64, input EditWorkInput) (EditWorkResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.editor == nil {
		return EditWorkResult{}, unavailableError()
	}
	end, err := a.workOperations.BeginEdit(id)
	if err != nil {
		if errors.Is(err, application.ErrWorkClosing) {
			return EditWorkResult{}, unavailableError()
		}
		return EditWorkResult{}, &APIError{Code: "work_busy", Message: "この作品は処理中です。処理が終わってから、もう一度保存してください。"}
	}
	defer end()
	edit := manga.EditWorkInput{URL: input.URL, Title: input.Title, SiteName: input.SiteName, Notes: input.Notes}
	if input.ScheduledDate != nil {
		date := manga.ScheduledDate(*input.ScheduledDate)
		if err := date.Validate(); err != nil {
			return EditWorkResult{}, &APIError{Code: "invalid_input", Message: err.Error()}
		}
		edit.ScheduledDate = &date
	}
	if input.Tags != nil {
		existing, err := tagSnapshots(input.Tags.Existing)
		if err != nil {
			return EditWorkResult{}, invalidTagInput()
		}
		tags, err := (manga.TagEdit{Existing: existing, NewNames: input.Tags.NewNames}).Normalize()
		if err != nil {
			return EditWorkResult{}, invalidTagInput()
		}
		edit.Tags = &tags
	}
	work, err := a.editor.EditWork(a.ctx, id, edit)
	a.coverOperations.ObserveError(err)
	if errors.Is(err, storage.ErrWorkEditOutcomeUnknown) {
		return EditWorkResult{}, &APIError{Code: "work_edit_outcome_unknown", Message: "作品情報の保存結果を確認できません。入力を控え、アプリを再起動して状態を確認してください。新たなデータの読み書きを停止しています。"}
	}
	if errors.Is(err, storage.ErrWorkEditCommitted) {
		return EditWorkResult{Work: workDTO(work), Warnings: []WorkWarningDTO{{Code: "database_stopped_after_commit", Message: "作品情報は保存しましたが、接続の解放を確認できません。新たなデータの読み書きを停止しています。アプリを再起動して状態を確認してください。"}}}, nil
	}
	if errors.Is(err, storage.ErrWorkTagsChanged) && !errors.Is(err, storage.ErrDBStopped) {
		return EditWorkResult{}, &APIError{Code: "tags_changed", Message: "選択したタグが変更または削除されました。保存していません。入力を残したままタグを再読み込みし、変更されたタグを確認してください。"}
	}
	if err != nil {
		return EditWorkResult{}, workAPIError(err)
	}
	return EditWorkResult{Work: workDTO(work), Warnings: []WorkWarningDTO{}}, nil
}
