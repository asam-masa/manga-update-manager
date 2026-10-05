package manga

import (
	"errors"
	"net/url"
	"path"
	"strings"
	"time"
)

var (
	ErrDuplicateURL         = errors.New("この作品URLは登録済みです")
	ErrURLRequired          = errors.New("作品URLを入力してください")
	ErrURLScheme            = errors.New("作品URLはhttpまたはhttpsで入力してください")
	ErrTitleRequired        = errors.New("タイトルを入力してください")
	ErrThumbnailPathInvalid = errors.New("サムネイルはアプリ管理領域内の相対パスで指定してください")
	ErrTimestampInvalid     = errors.New("作成日時と更新日時を指定してください")
)

// Work is a manga title managed by the application.
type Work struct {
	ID            int64
	URL           string
	Title         string
	SiteName      string
	ThumbnailPath string
	Notes         string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// NewWorkInput contains values entered when a work is registered.
type NewWorkInput struct {
	URL           string
	Title         string
	SiteName      string
	ThumbnailPath string
	Notes         string
}

// NewWork validates and normalizes values for a new work.
func NewWork(input NewWorkInput, now time.Time) (Work, error) {
	work := Work{
		URL:           strings.TrimSpace(input.URL),
		Title:         strings.TrimSpace(input.Title),
		SiteName:      strings.TrimSpace(input.SiteName),
		ThumbnailPath: normalizeThumbnailPath(input.ThumbnailPath),
		Notes:         strings.TrimSpace(input.Notes),
		CreatedAt:     now.Round(0).UTC(),
		UpdatedAt:     now.Round(0).UTC(),
	}

	if err := work.Validate(); err != nil {
		return Work{}, err
	}
	return work, nil
}

// Validate verifies invariants shared by creation and persistence.
func (w Work) Validate() error {
	if w.URL == "" {
		return ErrURLRequired
	}

	parsed, err := url.Parse(w.URL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ErrURLScheme
	}

	if w.Title == "" {
		return ErrTitleRequired
	}
	if w.ThumbnailPath != "" && !validThumbnailPath(w.ThumbnailPath) {
		return ErrThumbnailPathInvalid
	}
	if w.CreatedAt.IsZero() || w.UpdatedAt.IsZero() || w.UpdatedAt.Before(w.CreatedAt) {
		return ErrTimestampInvalid
	}
	return nil
}

func normalizeThumbnailPath(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), `\`, "/")
	if value == "" {
		return ""
	}
	return path.Clean(value)
}

func validThumbnailPath(value string) bool {
	if path.IsAbs(value) || value == "." || value == ".." || strings.HasPrefix(value, "../") {
		return false
	}
	firstSegment := strings.SplitN(value, "/", 2)[0]
	return !strings.Contains(firstSegment, ":")
}
