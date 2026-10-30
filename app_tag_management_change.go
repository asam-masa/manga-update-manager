package main

import (
	"errors"
	"strconv"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

// Tag is the confirmed renamed/deleted identity, not a post-commit reload.
type TagChangeResult struct {
	Tag      TagDTO           `json:"tag"`
	Warnings []WorkWarningDTO `json:"warnings"`
}

func (a *App) RenameTag(expected TagDTO, name string) (TagChangeResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.tagManager == nil {
		return TagChangeResult{}, unavailableError()
	}
	tags, err := tagSnapshots([]TagDTO{expected})
	if err != nil {
		return TagChangeResult{}, invalidTagInput()
	}
	if _, err := manga.NormalizeTagName(name); err != nil {
		return TagChangeResult{}, invalidTagInput()
	}
	end, err := a.workOperations.BeginTagChange(tags[0].ID)
	if err != nil {
		return TagChangeResult{}, tagAdmissionError(err)
	}
	defer end()
	tag, err := a.tagManager.RenameTag(a.ctx, tags[0], name)
	return a.tagChangeResult(tag, err)
}

func (a *App) DeleteTag(expected TagDeletePreviewDTO) (TagChangeResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.tagManager == nil {
		return TagChangeResult{}, unavailableError()
	}
	preview, err := tagDeleteConfirmation(expected)
	if err != nil {
		return TagChangeResult{}, invalidTagInput()
	}
	end, err := a.workOperations.BeginTagChange(preview.Tag.ID)
	if err != nil {
		return TagChangeResult{}, tagAdmissionError(err)
	}
	defer end()
	tag, err := a.tagManager.DeleteTag(a.ctx, preview)
	return a.tagChangeResult(tag, err)
}

func tagDeleteConfirmation(value TagDeletePreviewDTO) (storage.TagDeletePreview, error) {
	tags, err := tagSnapshots([]TagDTO{value.Tag})
	if err != nil || value.Works == nil {
		return storage.TagDeletePreview{}, storage.ErrTagConfirmation
	}
	result := storage.TagDeletePreview{Tag: tags[0], Works: make([]storage.TagWork, 0, len(value.Works))}
	ids := make(map[int64]bool)
	for _, work := range value.Works {
		id, idErr := strconv.ParseInt(work.ID, 10, 64)
		at, atErr := time.Parse(time.RFC3339Nano, work.CreatedAt)
		if idErr != nil || id <= 0 || strconv.FormatInt(id, 10) != work.ID || ids[id] || atErr != nil || at.IsZero() || at.Year() < 1 || at.Year() > 9999 || at.Format(time.RFC3339Nano) != work.CreatedAt {
			return storage.TagDeletePreview{}, storage.ErrTagConfirmation
		}
		ids[id] = true
		// Title is display-only; identity is the ID and the precise instant.
		result.Works = append(result.Works, storage.TagWork{ID: id, CreatedAt: at, Title: work.Title})
	}
	return result, nil
}

func tagAdmissionError(err error) error {
	if errors.Is(err, application.ErrWorkClosing) {
		return unavailableError()
	}
	return &APIError{Code: "tag_busy", Message: "このタグは変更中です。処理結果を確認してから、もう一度操作してください。"}
}

func (a *App) tagChangeResult(tag manga.Tag, err error) (TagChangeResult, error) {
	a.coverOperations.ObserveError(err)
	if errors.Is(err, storage.ErrTagChangeOutcomeUnknown) {
		return TagChangeResult{}, &APIError{Code: "tag_change_outcome_unknown", Message: "タグの変更結果を確認できません。新たなデータの読み書きを停止しています。操作を繰り返さず、入力を控えてアプリを再起動し、状態を確認してください。"}
	}
	committed := errors.Is(err, storage.ErrTagChangeCommitted)
	if err != nil && !committed {
		if errors.Is(err, storage.ErrDBStopped) {
			return TagChangeResult{}, databaseStoppedError()
		}
		if errors.Is(err, storage.ErrTagChanged) {
			return TagChangeResult{}, &APIError{Code: "tag_changed", Message: "確認後にタグ名または対象作品が変わりました。変更していません。入力を残し、最新情報を読み込んで内容を確認してください。"}
		}
		if errors.Is(err, storage.ErrTagDuplicateName) {
			return TagChangeResult{}, &APIError{Code: "tag_duplicate_name", Message: "同じ名前のタグが存在します。入力を残し、別のタグ名を指定してください。"}
		}
		if errors.Is(err, storage.ErrTagNotFound) {
			return TagChangeResult{}, tagManagementReadError(err)
		}
		return TagChangeResult{}, &APIError{Code: "storage_error", Message: "タグを変更できません。入力を控え、保存先を確認してアプリを再起動してください。"}
	}
	tags, conversionErr := tagDTOs([]manga.Tag{tag})
	if conversionErr != nil {
		return TagChangeResult{}, tagManagementReadError(nil)
	}
	result := TagChangeResult{Tag: tags[0], Warnings: []WorkWarningDTO{}}
	if committed {
		result.Warnings = append(result.Warnings, WorkWarningDTO{Code: "database_stopped_after_commit", Message: "タグの変更は確定しましたが、接続の解放を確認できません。新たなデータの読み書きを停止しています。アプリを再起動して状態を確認してください。"})
	}
	return result, nil
}
