package main

import (
	"errors"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

// The new read DTO uses string IDs; existing WorkDTO and mutation APIs stay intact.
type LibraryWorkDTO struct {
	ID             string  `json:"id"`
	URL            string  `json:"url"`
	Title          string  `json:"title"`
	SiteName       string  `json:"siteName"`
	ThumbnailPath  string  `json:"thumbnailPath"`
	Notes          string  `json:"notes"`
	CreatedAt      string  `json:"createdAt"`
	UpdatedAt      string  `json:"updatedAt"`
	LastAccessedAt *string `json:"lastAccessedAt"`
	ScheduledDate  *string `json:"scheduledDate"`
}

type WorkTagLinkDTO struct {
	WorkID string `json:"workID"`
	TagID  string `json:"tagID"`
}

type LibrarySnapshotDTO struct {
	Works []LibraryWorkDTO `json:"works"`
	Tags  []TagDTO         `json:"tags"`
	Links []WorkTagLinkDTO `json:"links"`
}

func (a *App) LoadLibrarySnapshot() (LibrarySnapshotDTO, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.library == nil {
		return LibrarySnapshotDTO{}, unavailableError()
	}
	value, err := a.library.LoadLibrarySnapshot(a.ctx)
	if err != nil {
		a.coverOperations.ObserveError(err)
		return LibrarySnapshotDTO{}, libraryReadError(err)
	}
	return librarySnapshotDTO(value)
}

func librarySnapshotDTO(value storage.LibrarySnapshot) (LibrarySnapshotDTO, error) {
	invalid := func() (LibrarySnapshotDTO, error) { return LibrarySnapshotDTO{}, libraryReadError(nil) }
	if value.Works == nil || value.Tags == nil || value.Links == nil {
		return invalid()
	}
	result := LibrarySnapshotDTO{Works: make([]LibraryWorkDTO, 0, len(value.Works)), Tags: make([]TagDTO, 0, len(value.Tags)), Links: make([]WorkTagLinkDTO, 0, len(value.Links))}
	workIDs, tagIDs, names := make(map[int64]bool), make(map[int64]bool), make(map[string]bool)
	var previous int64
	for _, work := range value.Works {
		if work.ID <= previous || work.Validate() != nil || !utf8.ValidString(work.URL) || !utf8.ValidString(work.Title) ||
			!utf8.ValidString(work.SiteName) || !utf8.ValidString(work.ThumbnailPath) || !utf8.ValidString(work.Notes) ||
			!libraryTimeValid(work.CreatedAt) || !libraryTimeValid(work.UpdatedAt) ||
			(work.LastAccessedAt != nil && !libraryTimeValid(*work.LastAccessedAt)) {
			return invalid()
		}
		previous, workIDs[work.ID] = work.ID, true
		w := workDTO(work)
		result.Works = append(result.Works, LibraryWorkDTO{ID: strconv.FormatInt(work.ID, 10), URL: w.URL, Title: w.Title, SiteName: w.SiteName,
			ThumbnailPath: w.ThumbnailPath, Notes: w.Notes, CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt, LastAccessedAt: w.LastAccessedAt, ScheduledDate: w.ScheduledDate})
	}
	previous = 0
	for _, tag := range value.Tags {
		if tag.ID <= previous || tag.Name.Validate() != nil || names[string(tag.Name)] {
			return invalid()
		}
		previous, tagIDs[tag.ID], names[string(tag.Name)] = tag.ID, true, true
		result.Tags = append(result.Tags, TagDTO{ID: strconv.FormatInt(tag.ID, 10), Name: string(tag.Name)})
	}
	var last storage.WorkTagLink
	for _, link := range value.Links {
		if !workIDs[link.WorkID] || !tagIDs[link.TagID] || link.WorkID < last.WorkID || (link.WorkID == last.WorkID && link.TagID <= last.TagID) {
			return invalid()
		}
		last = link
		result.Links = append(result.Links, WorkTagLinkDTO{WorkID: strconv.FormatInt(link.WorkID, 10), TagID: strconv.FormatInt(link.TagID, 10)})
	}
	return result, nil
}

func libraryTimeValid(value time.Time) bool {
	if value.IsZero() {
		return false
	}
	japan := platform.InJapan(value)
	if japan.Year() < 1 || japan.Year() > 9999 {
		return false
	}
	roundTrip, err := time.Parse(time.RFC3339Nano, japan.Format(time.RFC3339Nano))
	return err == nil && roundTrip.Equal(value)
}

func libraryReadError(err error) error {
	if errors.Is(err, storage.ErrDBStopped) {
		return databaseStoppedError()
	}
	return &APIError{Code: "storage_error", Message: "作品とタグの一覧を読み込めません。一覧を再読み込みし、解消しない場合はアプリを再起動してください。"}
}
