package main

import (
	"errors"
	"strconv"

	"github.com/asam-masa/manga-update-manager/internal/application"
)

type BackupCancelDTO struct {
	ExecutionID string `json:"executionId"`
	Decision    string `json:"decision"`
}

type BackupOmittedImageDTO struct {
	ImageID string `json:"imageId"`
	Reason  string `json:"reason"`
}

type BackupOmittedImagesDTO struct {
	ExecutionID string                  `json:"executionId"`
	Page        int                     `json:"page"`
	PageSize    int                     `json:"pageSize"`
	Pages       int                     `json:"pages"`
	Total       int                     `json:"total"`
	Items       []BackupOmittedImageDTO `json:"items"`
}

type BackupOmittedWorkDTO struct {
	WorkID string `json:"workId"`
	Title  string `json:"title"`
}

type BackupOmittedWorksDTO struct {
	ExecutionID string                 `json:"executionId"`
	ImageID     string                 `json:"imageId"`
	Page        int                    `json:"page"`
	PageSize    int                    `json:"pageSize"`
	Pages       int                    `json:"pages"`
	Total       int                    `json:"total"`
	Items       []BackupOmittedWorkDTO `json:"items"`
}

// Canonical decimal strings preserve all uint64 IDs across JavaScript's number
// boundary. No trimming, signs, leading zeros, exponents or implicit coercion.
func parseBackupExecutionID(value string) (application.BackupExecutionID, error) {
	if len(value) == 0 || len(value) > 20 {
		return 0, errBackupDetailRequest
	}
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil || id == 0 || strconv.FormatUint(id, 10) != value {
		return 0, errBackupDetailRequest
	}
	return application.BackupExecutionID(id), nil
}

// Control and observation must not acquire normal-operation tickets: they must
// remain usable while saving has paused normal admission or stopped storage.
func (a *App) CancelBackup(executionID string) (BackupCancelDTO, error) {
	id, err := parseBackupExecutionID(executionID)
	if err != nil {
		return BackupCancelDTO{}, backupDetailAPIError(err)
	}
	decision := a.backupRuns.cancel(id)
	if decision == application.BackupCancelStale {
		return BackupCancelDTO{}, backupDetailAPIError(errBackupDetailStale)
	}
	return BackupCancelDTO{ExecutionID: executionID, Decision: string(decision)}, nil
}

func (a *App) GetBackupOmittedImages(executionID string, page int) (BackupOmittedImagesDTO, error) {
	id, err := parseBackupExecutionID(executionID)
	if err != nil {
		return BackupOmittedImagesDTO{}, backupDetailAPIError(err)
	}
	r, err := a.backupRuns.imagePage(id, page)
	if err != nil {
		return BackupOmittedImagesDTO{}, backupDetailAPIError(err)
	}
	items := make([]BackupOmittedImageDTO, 0, len(r.Items))
	for _, image := range r.Items {
		reason := image.Reason
		if reason != "missing" && reason != "corrupt" {
			reason = "unavailable"
		}
		items = append(items, BackupOmittedImageDTO{ImageID: image.ID, Reason: reason})
	}
	return BackupOmittedImagesDTO{ExecutionID: strconv.FormatUint(uint64(r.ID), 10), Page: r.Page,
		PageSize: r.PageSize, Pages: r.Pages, Total: r.Total, Items: items}, nil
}

func (a *App) GetBackupOmittedImageWorks(executionID, imageID string, page int) (BackupOmittedWorksDTO, error) {
	id, err := parseBackupExecutionID(executionID)
	if err != nil {
		return BackupOmittedWorksDTO{}, backupDetailAPIError(err)
	}
	r, err := a.backupRuns.workPage(id, imageID, page)
	if err != nil {
		return BackupOmittedWorksDTO{}, backupDetailAPIError(err)
	}
	items := make([]BackupOmittedWorkDTO, 0, len(r.Items))
	for _, work := range r.Items {
		items = append(items, BackupOmittedWorkDTO{WorkID: strconv.FormatInt(work.WorkID, 10), Title: work.Title})
	}
	return BackupOmittedWorksDTO{ExecutionID: strconv.FormatUint(uint64(r.ID), 10), ImageID: r.ImageID,
		Page: r.Page, PageSize: r.PageSize, Pages: r.Pages, Total: r.Total, Items: items}, nil
}

func backupDetailAPIError(err error) error {
	switch {
	case errors.Is(err, errBackupDetailRequest):
		return &APIError{Code: "invalid_input", Message: "バックアップの実行IDとページ指定を確認してください。"}
	case errors.Is(err, errBackupDetailStale):
		return &APIError{Code: "backup_result_expired", Message: "対象のバックアップ結果は保持されていません。現在の状況を取得し直してください。"}
	case errors.Is(err, errBackupDetailPending):
		return &APIError{Code: "backup_result_pending", Message: "バックアップの処理が続いています。結果が確定してから確認してください。"}
	case errors.Is(err, errBackupDetailUnobserved):
		return &APIError{Code: "backup_details_unobserved", Message: "この処理では除外画像や作品名の情報を確認できていません。結果の案内を確認してください。"}
	case errors.Is(err, errBackupDetailUnavailable):
		return &APIError{Code: "backup_work_names_unavailable", Message: "対象作品名の詳細を取得できませんでした。結果の警告を確認してください。"}
	case errors.Is(err, errBackupDetailImage):
		return &APIError{Code: "backup_image_not_found", Message: "対象の除外画像が見つかりません。除外画像の一覧を取得し直してください。"}
	default:
		return &APIError{Code: "backup_details_failed", Message: "バックアップ情報を取得できませんでした。現在の状況を取得し直してください。"}
	}
}
