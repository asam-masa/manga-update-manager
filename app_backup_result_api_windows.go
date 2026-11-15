package main

import (
	"context"
	"errors"
	"strconv"

	"github.com/asam-masa/manga-update-manager/internal/platform"
)

type BackupNoticeDTO struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type BackupResultDTO struct {
	ExecutionID          string            `json:"executionId"`
	Outcome              string            `json:"outcome"`
	Message              string            `json:"message"`
	CancellationObserved bool              `json:"cancellationObserved"`
	RetryAllowed         bool              `json:"retryAllowed"`
	Notices              []BackupNoticeDTO `json:"notices"`
}

// RetryAllowed describes only this retained result, not current start authority.
// Start must still check App lifetime, admission, destination and storage safety.
func (a *App) GetBackupResult(executionID string) (BackupResultDTO, error) {
	id, err := parseBackupExecutionID(executionID)
	if err != nil {
		return BackupResultDTO{}, backupDetailAPIError(err)
	}
	v := a.backupRuns.overview()
	if v.execution.ID != id {
		return BackupResultDTO{}, backupDetailAPIError(errBackupDetailStale)
	}
	if v.result == nil {
		return BackupResultDTO{}, backupDetailAPIError(errBackupDetailPending)
	}
	return backupResultDTO(strconv.FormatUint(uint64(v.execution.ID), 10), v.result), nil
}

func backupResultDTO(id string, r *backupResultOverview) BackupResultDTO {
	out := BackupResultDTO{ExecutionID: id, Outcome: "unknown", Notices: []BackupNoticeDTO{},
		Message: "保存の成否を確認できません。候補ファイルを保全し、再保存せず確認してください。再起動だけで成否は確定しません。"}
	known := true
	switch r.Save.Publication.State {
	case platform.BackupPublishConfirmed:
		out.Outcome, out.Message = "saved", "バックアップを保存しました。"
		if hasBackupResultWarnings(r) || !r.RetrySafetyConfirmed {
			out.Outcome, out.Message = "saved_with_warnings", "バックアップは保存されています。除外画像や後処理の警告を確認してください。"
		}
	case platform.BackupPublishCollision:
		out.Outcome, out.Message = "collision", "同じ名前のファイルがあり、上書きせず保存を中止しました。"
	case platform.BackupPublishNotAttempted:
		out.Outcome, out.Message = "not_saved", "バックアップは保存されていません。取消や処理の警告を確認してください。"
	default:
		known = false
	}
	out.RetryAllowed = known && r.RetrySafetyConfirmed && r.FinishError == nil
	out.CancellationObserved = errors.Is(r.Err, context.Canceled) || errors.Is(r.Database.OperationError, context.Canceled) || errors.Is(r.Database.CleanupError, context.Canceled)
	add := func(code, message string) {
		out.Notices = append(out.Notices, BackupNoticeDTO{Code: code, Message: message})
	}
	if out.Outcome == "not_saved" {
		add("processing_incomplete", "処理は完了していません。取消の有無と、保存先・画像・作業領域の状態を確認してください。")
	}
	if r.Save.Publication.State == platform.BackupPublishConfirmed && r.Err != nil {
		add("postprocess_warning", "保存は確認できていますが、後処理に警告があります。保存先と状態を確認してください。")
	}
	if out.CancellationObserved {
		add("cancellation_observed", "取消を検出しました。ほかの処理や片付けの警告も確認してください。")
	}
	if len(r.Warnings) != 0 {
		add("preparation_warning", "開始前の確認に警告があります。保存先と状態を確認してください。")
	}
	if r.OmittedImageCount != nil && *r.OmittedImageCount > 0 {
		add("images_omitted", "一部の画像を除外しています。対象作品と理由を確認してください。")
	}
	if r.DetailError != nil || (r.WorkNamesState != backupWorkNamesAvailable && r.WorkNamesState != backupWorkNamesUnobserved) {
		add("work_names_unavailable", "対象作品名の詳細を取得できませんでした。除外件数と理由を確認してください。")
	}
	if r.Database.CleanupError != nil || (r.Save.Cleanup != nil && !r.Save.Cleanup.Absent) || errors.Is(r.Err, platform.ErrBackupTempRelease) || errors.Is(r.Err, platform.ErrBackupDirectoryRelease) {
		add("cleanup_warning", "片付けや資源の解放に警告があります。作業物が残っている可能性があります。")
	}
	if !out.RetryAllowed {
		add("backup_retry_blocked", "再保存せず、保全対象や保存先を確認してください。再起動後も状態を確認してから操作してください。")
	}
	return out
}
