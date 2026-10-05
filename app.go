package main

import (
	"context"
	"errors"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

// App exposes the application boundary to the Wails frontend.
type App struct {
	mu            sync.RWMutex
	ctx           context.Context
	works         *application.Works
	store         *storage.Store
	dataDirectory func() (string, error)
}

// NewApp creates the Wails application boundary.
func NewApp() *App {
	return &App{dataDirectory: platform.DataDirectory}
}

// Status confirms that the frontend can call the Go boundary.
func (a *App) Status() string {
	return "ready"
}

func (a *App) startup(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.store != nil {
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
}

func (a *App) shutdown(_ context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.works = nil
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

func formatAPIError(err error) any {
	var apiError *APIError
	if errors.As(err, &apiError) {
		return apiError
	}
	return &APIError{Code: "internal_error", Message: "処理に失敗しました。アプリを再起動して再度お試しください。"}
}

func workAPIError(err error) error {
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
	ID            int64  `json:"id"`
	URL           string `json:"url"`
	Title         string `json:"title"`
	SiteName      string `json:"siteName"`
	ThumbnailPath string `json:"thumbnailPath"`
	Notes         string `json:"notes"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
}

func workDTO(work manga.Work) WorkDTO {
	return WorkDTO{
		ID: work.ID, URL: work.URL, Title: work.Title, SiteName: work.SiteName,
		ThumbnailPath: work.ThumbnailPath, Notes: work.Notes,
		CreatedAt: platform.InJapan(work.CreatedAt).Format(time.RFC3339Nano),
		UpdatedAt: platform.InJapan(work.UpdatedAt).Format(time.RFC3339Nano),
	}
}

func (a *App) CreateWork(input CreateWorkInput) (WorkDTO, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.works == nil {
		return WorkDTO{}, unavailableError()
	}
	work, err := a.works.CreateWork(a.ctx, manga.NewWorkInput{URL: input.URL, Title: input.Title, SiteName: input.SiteName, ThumbnailPath: input.ThumbnailPath, Notes: input.Notes})
	if err != nil {
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
		return nil, workAPIError(err)
	}
	result := make([]WorkDTO, 0, len(works))
	for _, work := range works {
		result = append(result, workDTO(work))
	}
	return result, nil
}
