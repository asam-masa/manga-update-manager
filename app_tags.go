package main

import (
	"errors"
	"strconv"

	"github.com/asam-masa/manga-update-manager/internal/manga"
)

// New tag IDs are decimal strings so JS never rounds an int64 identifier.
// Existing work IDs and WorkDTO remain unchanged for compatibility.
type TagDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type TagEditInput struct {
	Existing []TagDTO `json:"existing"`
	NewNames []string `json:"newNames"`
}

func (a *App) LoadTags() ([]TagDTO, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.tags == nil {
		return nil, unavailableError()
	}
	tags, err := a.tags.ListTags(a.ctx)
	if err != nil {
		a.coverOperations.ObserveError(err)
		return nil, workAPIError(err)
	}
	return tagDTOs(tags)
}

func (a *App) LoadWorkTags(id int64) ([]TagDTO, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.tags == nil {
		return nil, unavailableError()
	}
	tags, err := a.tags.WorkTags(a.ctx, id)
	if err != nil {
		a.coverOperations.ObserveError(err)
		return nil, workAPIError(err)
	}
	return tagDTOs(tags)
}

func tagDTOs(tags []manga.Tag) ([]TagDTO, error) {
	result := make([]TagDTO, 0, len(tags))
	for _, tag := range tags {
		if tag.ID <= 0 || tag.Name.Validate() != nil {
			return nil, workAPIError(errors.New("invalid stored tag"))
		}
		result = append(result, TagDTO{ID: strconv.FormatInt(tag.ID, 10), Name: string(tag.Name)})
	}
	return result, nil
}

// Incoming names are exact snapshots, not new names to trim or repair.
func tagSnapshots(values []TagDTO) ([]manga.Tag, error) {
	result := make([]manga.Tag, 0, len(values))
	for _, value := range values {
		id, err := strconv.ParseInt(value.ID, 10, 64)
		name := manga.TagName(value.Name)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != value.ID || name.Validate() != nil {
			return nil, manga.ErrTagSelection
		}
		result = append(result, manga.Tag{ID: id, Name: name})
	}
	return result, nil
}

func invalidTagInput() error {
	return &APIError{Code: "invalid_input", Message: "タグの選択と名前を確認してください。タグ名は前後の空白を除いて1〜50文字で、改行・制御文字を含めず入力してください。"}
}
