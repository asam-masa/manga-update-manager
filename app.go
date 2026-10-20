package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

// App exposes the application boundary to the Wails frontend.
type App struct {
	mu                     sync.RWMutex
	ctx                    context.Context
	works                  *application.Works
	display                *application.DisplaySettings
	opener                 *application.WorkOpener
	covers                 *application.CoverImporter
	coverReader            coverHTTPReader
	coverOperations        application.CoverOperations
	coverProgress          coverProgressState
	chooseCover            func(context.Context) (string, error)
	coverSelectionFinished func(context.Context, int64, string)
	coverBusyNotice        func(context.Context)
	openURL                func(string) error
	store                  *storage.Store
	dataDirectory          func() (string, error)
	stopped                bool
	startupFinished        chan struct{}
	startupFinishedOnce    sync.Once
	coverCleanupOnce       sync.Once
	coverCleanupPhase      atomic.Uint32 // 0: pending, 1: claimed, 2: cancelled before start
	coverCleanupTicket     *application.CoverOperation
	coverCleanupWarnings   []CoverWarningDTO
}

// NewApp creates the Wails application boundary.
func NewApp() *App {
	return &App{dataDirectory: platform.DataDirectory, openURL: platform.OpenURL, chooseCover: chooseCover, coverSelectionFinished: emitCoverSelectionFinished, coverBusyNotice: coverBusyNotice, startupFinished: make(chan struct{})}
}

// Status confirms that the frontend can call the Go boundary.
func (a *App) Status() string {
	return "ready"
}

func (a *App) startup(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	defer a.startupFinishedOnce.Do(func() { close(a.startupFinished) })
	if a.stopped || a.store != nil {
		return
	}
	a.ctx = ctx
	directory, err := a.dataDirectory()
	if err != nil {
		log.Print("アプリデータ保存先を初期化できません")
		return
	}
	store, err := storage.Open(ctx, filepath.Join(directory, "manga.sqlite"))
	if err != nil {
		log.Print("作品データベースを初期化できません")
		return
	}
	a.store = store
	a.works = application.NewWorks(store, time.Now)
	a.display = application.NewDisplaySettings(store)
	a.opener = application.NewWorkOpener(store, a.openURL, time.Now)
	a.covers = application.NewCoverImporter(store, &a.coverOperations, directory, a.chooseCover, time.Now)
	a.coverReader = application.NewCoverReader(store, &a.coverOperations, directory)
	a.coverCleanupTicket, err = a.coverOperations.BeginCleanup()
	if err != nil {
		a.coverCleanupWarnings = []CoverWarningDTO{{Code: "cover_cleanup_failed", Message: "画像整理を開始できませんでした。アプリを再起動して状態を確認してください。"}}
	}
}

func (a *App) shutdown(_ context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	// Wails starts OnStartup asynchronously. A queued callback must never
	// reopen storage after shutdown (and the production storage lock release).
	a.stopped = true
	a.coverCleanupPhase.CompareAndSwap(0, 2)
	a.startupFinishedOnce.Do(func() { close(a.startupFinished) })
	if a.coverCleanupTicket != nil {
		_ = a.coverCleanupTicket.End()
	}
	a.works = nil
	a.display = nil
	a.opener = nil
	a.covers = nil
	a.coverReader = nil
	if a.store != nil {
		if err := a.store.Close(); err != nil {
			log.Print("作品データベースを閉じられませんでした")
		}
		a.store = nil
	}
}

// APIError is formatted as a structured Wails Promise rejection.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string { return e.Message }

const apiErrorWirePrefix = "manga-update-manager:api-error:v1:"
const apiErrorWireLimit = 8192

func formatAPIError(err error) any {
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError == nil {
		apiError = &APIError{Code: "internal_error", Message: "処理に失敗しました。アプリを再起動して再度お試しください。"}
	}
	// Wails wraps the formatted value with new Error(value). A string survives;
	// an object loses its fields through JavaScript's default string conversion.
	payload, marshalErr := json.Marshal(apiError)
	if marshalErr != nil || len(apiErrorWirePrefix)+len(payload) > apiErrorWireLimit {
		return apiErrorWirePrefix + `{"code":"internal_error","message":"処理に失敗しました。アプリを再起動して再度お試しください。"}`
	}
	return apiErrorWirePrefix + string(payload)
}

func workAPIError(err error) error {
	if errors.Is(err, storage.ErrDBStopped) {
		if errors.Is(err, application.ErrAccessSave) {
			return &APIError{Code: "access_save_failed", Message: "ページを開きましたが、最終アクセス日時の保存を確認できません。データの安全性を確認できないため、新たな読み書きを停止しています。アプリを再起動して状態を確認してください。"}
		}
		return databaseStoppedError()
	}
	for _, entry := range []struct {
		err           error
		code, message string
	}{
		{application.ErrAccessSave, "access_save_failed", "ページを開きましたが、最終アクセス日時を保存できませんでした。保存先を確認してください。"},
		{manga.ErrWorkNotFound, "work_not_found", "作品が見つかりません。一覧を再読み込みしてください。"},
		{application.ErrBrowserOpen, "browser_open_failed", "ページを開けませんでした。既定ブラウザーの設定を確認して再度お試しください。"},
		{application.ErrOpenInProgress, "work_open_in_progress", "この作品を開いています。処理が終わるまでお待ちください。"},
	} {
		if errors.Is(err, entry.err) {
			return &APIError{Code: entry.code, Message: entry.message}
		}
	}
	if errors.Is(err, manga.ErrDuplicateURL) {
		return &APIError{Code: "duplicate_url", Message: manga.ErrDuplicateURL.Error()}
	}
	for _, validation := range []error{manga.ErrURLRequired, manga.ErrURLScheme, manga.ErrTitleRequired, manga.ErrThumbnailPathInvalid, manga.ErrTimestampInvalid} {
		if errors.Is(err, validation) {
			return &APIError{Code: "invalid_input", Message: validation.Error()}
		}
	}
	return &APIError{Code: "storage_error", Message: "作品データを読み書きできません。保存先の空き容量とアクセス権限を確認し、アプリを再起動してください。"}
}

func databaseStoppedError() error {
	return &APIError{Code: "database_stopped", Message: "データの安全性を確認できないため、新たな読み書きを停止しています。アプリを再起動して状態を確認してください。表示中の一覧は最新とは限りません。"}
}

func unavailableError() error {
	return &APIError{Code: "database_unavailable", Message: "作品データを利用できません。アプリデータ保存先の空き容量とアクセス権限を確認し、アプリを再起動してください。"}
}

type CreateWorkInput struct {
	URL           string `json:"url"`
	Title         string `json:"title"`
	SiteName      string `json:"siteName"`
	ThumbnailPath string `json:"thumbnailPath"`
	Notes         string `json:"notes"`
}

type WorkDTO struct {
	ID             int64   `json:"id"`
	URL            string  `json:"url"`
	Title          string  `json:"title"`
	SiteName       string  `json:"siteName"`
	ThumbnailPath  string  `json:"thumbnailPath"`
	Notes          string  `json:"notes"`
	CreatedAt      string  `json:"createdAt"`
	UpdatedAt      string  `json:"updatedAt"`
	LastAccessedAt *string `json:"lastAccessedAt"`
}

func workDTO(work manga.Work) WorkDTO {
	var accessed *string
	if work.LastAccessedAt != nil {
		value := platform.InJapan(*work.LastAccessedAt).Format(time.RFC3339Nano)
		accessed = &value
	}
	return WorkDTO{
		ID: work.ID, URL: work.URL, Title: work.Title, SiteName: work.SiteName,
		ThumbnailPath: work.ThumbnailPath, Notes: work.Notes,
		CreatedAt:      platform.InJapan(work.CreatedAt).Format(time.RFC3339Nano),
		UpdatedAt:      platform.InJapan(work.UpdatedAt).Format(time.RFC3339Nano),
		LastAccessedAt: accessed,
	}
}

func (a *App) OpenWork(id int64) (WorkDTO, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.opener == nil {
		return WorkDTO{}, unavailableError()
	}
	work, err := a.opener.OpenWork(a.ctx, id)
	if err != nil {
		a.coverOperations.ObserveError(err)
		return WorkDTO{}, workAPIError(err)
	}
	return workDTO(work), nil
}

func (a *App) CreateWork(input CreateWorkInput) (WorkDTO, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.works == nil {
		return WorkDTO{}, unavailableError()
	}
	// Registration can supply a thumbnail reference too; it must not race
	// managed-image reference changes or future unreferenced-file cleanup.
	unlock, err := a.coverOperations.LockReferences(a.ctx)
	if err != nil {
		return WorkDTO{}, workAPIError(err)
	}
	defer unlock()
	work, err := a.works.CreateWork(a.ctx, manga.NewWorkInput{URL: input.URL, Title: input.Title, SiteName: input.SiteName, ThumbnailPath: input.ThumbnailPath, Notes: input.Notes})
	if err != nil {
		a.coverOperations.ObserveError(err)
		return WorkDTO{}, workAPIError(err)
	}
	return workDTO(work), nil
}

func (a *App) ListWorks() ([]WorkDTO, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.works == nil {
		return nil, unavailableError()
	}
	works, err := a.works.ListWorks(a.ctx)
	if err != nil {
		a.coverOperations.ObserveError(err)
		return nil, workAPIError(err)
	}
	result := make([]WorkDTO, 0, len(works))
	for _, work := range works {
		result = append(result, workDTO(work))
	}
	return result, nil
}
