package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
)

// ErrWorkCoverOutcomeUnknown marks a failed Commit, not a confirmed rollback.
// Preserve images; callers must not retry or infer connection health from it.
// Its absence does not prove that resource cleanup succeeded.
var ErrWorkCoverOutcomeUnknown = errors.New("work cover outcome unknown")

// ErrWorkCoverCommitted marks a confirmed Commit followed by cleanup failure.
// The returned work/previous path are committed results, never deletion authority.
var ErrWorkCoverCommitted = errors.New("work cover committed with cleanup failure")

// SetWorkCover attaches a valid ready record, not an arbitrary path.
// The previous path is not authorization to delete a file.
func (s *Store) SetWorkCover(ctx context.Context, workID int64, coverID string, at time.Time) (manga.Work, string, error) {
	if !lowerHex(coverID, 32) {
		return manga.Work{}, "", ErrInvalidManagedCover
	}
	return s.changeWorkCover(ctx, workID, coverID, at)
}

// RemoveWorkCover clears only the reference. It never deletes files or records.
func (s *Store) RemoveWorkCover(ctx context.Context, workID int64, at time.Time) (manga.Work, string, error) {
	return s.changeWorkCover(ctx, workID, "", at)
}

func (s *Store) changeWorkCover(ctx context.Context, workID int64, coverID string, at time.Time) (_ manga.Work, _ string, err error) {
	if at.IsZero() {
		return manga.Work{}, "", manga.ErrTimestampInvalid
	}
	conn, finish, err := s.begin(ctx)
	if err != nil {
		return manga.Work{}, "", err
	}
	committed := false
	defer func() {
		finish(&err)
		if committed && err != nil {
			err = errors.Join(ErrWorkCoverCommitted, err)
		}
	}()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return manga.Work{}, "", fmt.Errorf("begin work cover change: %w", err)
	}
	finalized := false
	defer func() {
		if !finalized {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				s.admission.stop()
				err = errors.Join(err, ErrDBStopped, rollbackErr)
			}
		}
	}()
	work, err := scanWork(queryOne(ctx, tx, workByIDQuery, workID))
	if err != nil {
		return manga.Work{}, "", err
	}
	if work.CreatedAt.IsZero() || work.UpdatedAt.IsZero() || work.UpdatedAt.Before(work.CreatedAt) {
		return manga.Work{}, "", manga.ErrTimestampInvalid
	}
	nextPath := ""
	if coverID != "" {
		cover, err := scanManagedCover(queryOne(ctx, tx, managedCoverByIDQuery, coverID), coverID)
		if err != nil {
			return manga.Work{}, "", err
		}
		if cover.State != "ready" {
			return manga.Work{}, "", ErrInvalidManagedCover
		}
		nextPath = cover.RelativePath
	}
	previous := work.ThumbnailPath
	if previous != nextPath {
		at = at.Round(0).UTC()
		if at.Before(work.UpdatedAt) {
			at = work.UpdatedAt
		}
		result, err := tx.ExecContext(ctx, "UPDATE works SET thumbnail_path=?,updated_at=? WHERE id=?", nextPath, formatTime(at), workID)
		if err != nil {
			return manga.Work{}, "", fmt.Errorf("update work cover: %w", err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return manga.Work{}, "", fmt.Errorf("count changed work covers: %w", err)
		}
		if count != 1 {
			return manga.Work{}, "", fmt.Errorf("change work cover: expected 1 row, got %d", count)
		}
		work.ThumbnailPath, work.UpdatedAt = nextPath, at
	}
	commitErr := tx.Commit()
	finalized = true // Commit owns Tx completion, including driver-side cleanup.
	if commitErr != nil {
		s.admission.stop()
		return manga.Work{}, "", fmt.Errorf("commit work cover change: %w", errors.Join(ErrWorkCoverOutcomeUnknown, ErrDBStopped, commitErr))
	}
	committed = true
	return work, previous, nil
}
