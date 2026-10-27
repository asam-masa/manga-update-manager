package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

var (
	ErrWorkEditOutcomeUnknown = errors.New("work edit outcome unknown")
	ErrWorkEditCommitted      = errors.New("work edit committed with cleanup failure")
)

// EditWork changes editable content, based on the current stored row.
// Operation-level exclusion with image/open/delete belongs to its future caller.
func (s *Store) EditWork(ctx context.Context, id int64, input manga.EditWorkInput, at time.Time) (result manga.Work, err error) {
	input, err = input.Normalize()
	if err != nil {
		return manga.Work{}, err
	}
	if at.IsZero() {
		return manga.Work{}, manga.ErrTimestampInvalid
	}
	conn, finish, err := s.begin(ctx)
	if err != nil {
		return manga.Work{}, err
	}
	committed := false
	defer func() {
		finish(&err)
		if err != nil {
			if committed {
				err = errors.Join(ErrWorkEditCommitted, err)
			} else {
				result = manga.Work{}
			}
		}
	}()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return manga.Work{}, fmt.Errorf("begin work edit: %w", err)
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
	work, err := scanWork(queryOne(ctx, tx, workByIDQuery, id))
	if err != nil {
		return manga.Work{}, err
	}
	if err := work.Validate(); err != nil {
		return manga.Work{}, err
	}
	date := work.ScheduledDate
	if input.ScheduledDate != nil {
		date = *input.ScheduledDate
	}
	tagsChanged := false
	if input.Tags != nil {
		tagsChanged, err = syncWorkTags(ctx, tx, id, *input.Tags)
		if err != nil {
			return manga.Work{}, err
		}
	}
	if work.URL == input.URL && work.Title == input.Title && work.SiteName == input.SiteName && work.Notes == input.Notes && work.ScheduledDate == date && !tagsChanged {
		// Read-only transaction: release through Rollback, never an empty Commit.
		return work, nil
	}
	at = at.Round(0).UTC()
	if at.Before(work.UpdatedAt) {
		at = work.UpdatedAt
	}
	updated, err := tx.ExecContext(ctx, "UPDATE works SET url=?,title=?,site_name=?,notes=?,scheduled_date=?,updated_at=? WHERE id=?", input.URL, input.Title, input.SiteName, input.Notes, scheduledDateValue(date), formatTime(at), id)
	if err != nil {
		var sqliteErr *sqlite.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
			return manga.Work{}, manga.ErrDuplicateURL
		}
		return manga.Work{}, fmt.Errorf("update work text: %w", err)
	}
	count, err := updated.RowsAffected()
	if err != nil {
		return manga.Work{}, fmt.Errorf("count work edits: %w", err)
	}
	if count != 1 {
		return manga.Work{}, fmt.Errorf("edit work: expected 1 row, got %d", count)
	}
	commitErr := tx.Commit()
	finalized = true
	if commitErr != nil {
		s.admission.stop()
		return manga.Work{}, fmt.Errorf("commit work edit: %w", errors.Join(ErrWorkEditOutcomeUnknown, ErrDBStopped, commitErr))
	}
	committed = true
	work.URL, work.Title, work.SiteName, work.Notes = input.URL, input.Title, input.SiteName, input.Notes
	work.ScheduledDate = date
	work.UpdatedAt = at
	return work, nil
}
