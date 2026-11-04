package main

import (
	"errors"

	"github.com/asam-masa/manga-update-manager/internal/application"
)

// Admit before App/work/reference locks, and defer this release first so it
// runs after all existing defers. This gate does not grant DB/image permission.
func (a *App) beginNormalOperation() (func(), error) {
	end, err := a.operationGate.BeginNormal()
	if errors.Is(err, application.ErrOperationsPaused) {
		return nil, &APIError{Code: "backup_in_progress", Message: "バックアップの開始待ちまたは処理中です。完了または取消の結果を確認してから、もう一度操作してください。"}
	}
	if err != nil {
		return nil, unavailableError()
	}
	return end, nil
}
