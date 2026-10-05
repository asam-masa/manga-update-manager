package manga

import (
	"errors"
	"testing"
	"time"
)

func TestNewWorkNormalizesInputAndUTC(t *testing.T) {
	now := time.Date(2026, 10, 5, 9, 30, 0, 123, time.FixedZone("JST", 9*60*60))

	work, err := NewWork(NewWorkInput{
		URL:           "  https://example.com/manga/1  ",
		Title:         "  作品名  ",
		SiteName:      "  サイト名  ",
		ThumbnailPath: ` covers\1.webp `,
		Notes:         "  メモ  ",
	}, now)
	if err != nil {
		t.Fatalf("NewWork() error = %v", err)
	}

	if work.URL != "https://example.com/manga/1" {
		t.Errorf("URL = %q", work.URL)
	}
	if work.Title != "作品名" || work.SiteName != "サイト名" || work.Notes != "メモ" {
		t.Errorf("text values were not normalized: %#v", work)
	}
	if work.ThumbnailPath != "covers/1.webp" {
		t.Errorf("ThumbnailPath = %q", work.ThumbnailPath)
	}
	if work.CreatedAt.Location() != time.UTC || !work.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v", work.CreatedAt)
	}
	if !work.UpdatedAt.Equal(work.CreatedAt) {
		t.Errorf("UpdatedAt = %v, CreatedAt = %v", work.UpdatedAt, work.CreatedAt)
	}
}

func TestNewWorkRejectsInvalidInput(t *testing.T) {
	now := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		input NewWorkInput
		want  error
	}{
		{name: "URL is required", input: NewWorkInput{Title: "作品"}, want: ErrURLRequired},
		{name: "title is required", input: NewWorkInput{URL: "https://example.com"}, want: ErrTitleRequired},
		{name: "scheme is restricted", input: NewWorkInput{URL: "file:///tmp/work", Title: "作品"}, want: ErrURLScheme},
		{name: "host is required", input: NewWorkInput{URL: "https:///work", Title: "作品"}, want: ErrURLScheme},
		{name: "absolute thumbnail is rejected", input: NewWorkInput{URL: "https://example.com", Title: "作品", ThumbnailPath: "C:/covers/1.webp"}, want: ErrThumbnailPathInvalid},
		{name: "parent thumbnail is rejected", input: NewWorkInput{URL: "https://example.com", Title: "作品", ThumbnailPath: "../1.webp"}, want: ErrThumbnailPathInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewWork(tt.input, now)
			if !errors.Is(err, tt.want) {
				t.Fatalf("NewWork() error = %v, want %v", err, tt.want)
			}
		})
	}
}
