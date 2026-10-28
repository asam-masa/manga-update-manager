package main

import (
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

type TagUsageDTO struct {
	Tag       TagDTO `json:"tag"`
	WorkCount int64  `json:"workCount"`
}

// New confirmation IDs are strings; existing WorkDTO is unchanged.
type TagWorkDTO struct {
	ID        string `json:"id"`
	CreatedAt string `json:"createdAt"`
	Title     string `json:"title"`
}

type TagDeletePreviewDTO struct {
	Tag   TagDTO       `json:"tag"`
	Works []TagWorkDTO `json:"works"`
}

func (a *App) LoadTagUsage() ([]TagUsageDTO, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.tagManagement == nil {
		return nil, unavailableError()
	}
	values, err := a.tagManagement.ListTagUsage(a.ctx)
	if err != nil {
		a.coverOperations.ObserveError(err)
		return nil, tagManagementReadError(err)
	}
	result := make([]TagUsageDTO, 0, len(values))
	ids, names := make(map[int64]bool), make(map[string]bool)
	for _, value := range values {
		tags, err := tagDTOs([]manga.Tag{value.Tag})
		if err != nil || ids[value.Tag.ID] || names[string(value.Tag.Name)] || value.WorkCount < 0 || value.WorkCount > 1<<53-1 {
			return nil, tagManagementReadError(nil)
		}
		ids[value.Tag.ID], names[string(value.Tag.Name)] = true, true
		result = append(result, TagUsageDTO{Tag: tags[0], WorkCount: value.WorkCount})
	}
	return result, nil
}

func (a *App) LoadTagDeletePreview(id string) (TagDeletePreviewDTO, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.tagManagement == nil {
		return TagDeletePreviewDTO{}, unavailableError()
	}
	parsed, err := strconv.ParseInt(id, 10, 64)
	if err != nil || parsed <= 0 || strconv.FormatInt(parsed, 10) != id {
		return TagDeletePreviewDTO{}, invalidTagInput()
	}
	value, err := a.tagManagement.PreviewTagDelete(a.ctx, parsed)
	if err != nil {
		a.coverOperations.ObserveError(err)
		return TagDeletePreviewDTO{}, tagManagementReadError(err)
	}
	tags, err := tagDTOs([]manga.Tag{value.Tag})
	if err != nil || value.Tag.ID != parsed || value.Works == nil {
		return TagDeletePreviewDTO{}, tagManagementReadError(nil)
	}
	result := TagDeletePreviewDTO{Tag: tags[0], Works: make([]TagWorkDTO, 0, len(value.Works))}
	ids := make(map[int64]bool)
	for _, work := range value.Works {
		created := platform.InJapan(work.CreatedAt).Format(time.RFC3339Nano)
		at, err := time.Parse(time.RFC3339Nano, created)
		if work.ID <= 0 || ids[work.ID] || work.CreatedAt.IsZero() || err != nil || !at.Equal(work.CreatedAt) || at.Year() < 1 || strings.TrimSpace(work.Title) == "" || !utf8.ValidString(work.Title) {
			return TagDeletePreviewDTO{}, tagManagementReadError(nil)
		}
		ids[work.ID] = true
		result.Works = append(result.Works, TagWorkDTO{ID: strconv.FormatInt(work.ID, 10), CreatedAt: created, Title: work.Title})
	}
	return result, nil
}

func tagManagementReadError(err error) error {
	if errors.Is(err, storage.ErrDBStopped) {
		return databaseStoppedError()
	}
	if errors.Is(err, storage.ErrTagNotFound) {
		return &APIError{Code: "tag_not_found", Message: "タグが見つかりません。タグ一覧を再読み込みしてください。"}
	}
	return &APIError{Code: "storage_error", Message: "タグ情報を読み込めません。保存先を確認し、アプリを再起動してください。"}
}
