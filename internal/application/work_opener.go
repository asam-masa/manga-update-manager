package application

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
)

var (
	ErrBrowserOpen    = errors.New("browser launch failed")
	ErrAccessSave     = errors.New("access save failed after browser launch")
	ErrOpenInProgress = errors.New("work launch already in progress")
)

type AccessRepository interface {
	WorkByID(context.Context, int64) (manga.Work, error)
	RecordLastAccess(context.Context, int64, time.Time) error
}

type WorkOpener struct {
	repository AccessRepository
	openURL    func(string) error
	now        func() time.Time
	mu         sync.Mutex
	opening    map[int64]bool
}

func NewWorkOpener(repository AccessRepository, openURL func(string) error, now func() time.Time) *WorkOpener {
	return &WorkOpener{repository: repository, openURL: openURL, now: now, opening: make(map[int64]bool)}
}

func (a *WorkOpener) OpenWork(ctx context.Context, id int64) (manga.Work, error) {
	a.mu.Lock()
	if a.opening[id] {
		a.mu.Unlock()
		return manga.Work{}, ErrOpenInProgress
	}
	a.opening[id] = true
	a.mu.Unlock()
	defer func() { a.mu.Lock(); delete(a.opening, id); a.mu.Unlock() }()
	work, err := a.repository.WorkByID(ctx, id)
	if err != nil {
		return manga.Work{}, err
	}
	// Validate persisted input before crossing the OS boundary.
	if err := work.Validate(); err != nil {
		return manga.Work{}, err
	}
	if err := a.openURL(work.URL); err != nil {
		return manga.Work{}, ErrBrowserOpen
	}
	at := a.now().Round(0).UTC()
	if err := a.repository.RecordLastAccess(ctx, id, at); err != nil {
		return manga.Work{}, ErrAccessSave
	}
	work.LastAccessedAt = &at
	return work, nil
}
