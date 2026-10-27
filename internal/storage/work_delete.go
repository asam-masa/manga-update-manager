package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/asam-masa/manga-update-manager/internal/manga"
)

var (
	ErrWorkDeleteChanged        = errors.New("work changed since delete confirmation")
	ErrWorkDeleteOutcomeUnknown = errors.New("work delete outcome unknown")
	ErrWorkDeleteCommitted      = errors.New("work deleted with cleanup failure")
)

// DeleteWork deletes only the confirmed work row. It never deletes images or
// managed-cover records. The returned work is usable only after confirmed Commit;
// even then its image path is not permission to delete a file.
// App-level operation/reference exclusion belongs to the future caller.
func (s *Store) DeleteWork(ctx context.Context, id int64, expected manga.WorkDeleteInput) (result manga.Work, err error) {
	conn, finish, err := s.begin(ctx)
	if err != nil {
		return manga.Work{}, err
	}
	committed := false
	defer func() {
		finish(&err)
		result, err = workDeleteOutcome(result, err, committed)
	}()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return manga.Work{}, fmt.Errorf("begin work delete: %w", err)
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
	current := manga.WorkDeleteInput{URL: work.URL, Title: work.Title, SiteName: work.SiteName, Notes: work.Notes, ThumbnailPath: work.ThumbnailPath, ScheduledDate: work.ScheduledDate}
	textExpected := expected
	textExpected.Tags = nil
	if current != textExpected {
		return manga.Work{}, ErrWorkDeleteChanged
	}
	if expected.Tags != nil {
		tags, err := readTags(ctx, tx, workTagsQuery, id)
		if err != nil {
			return manga.Work{}, err
		}
		if !sameTags(tags, *expected.Tags) {
			return manga.Work{}, ErrWorkDeleteChanged
		}
	}
	// The transaction prevents a concurrent writer from changing the read row and
	// then having us delete that new content. The predicate also binds all fields
	// explicitly; neither modification time nor access time is a revision token.
	deleted, err := tx.ExecContext(ctx, `DELETE FROM works
 WHERE id=? AND url=? AND title=? AND site_name=? AND notes=? AND thumbnail_path=? AND scheduled_date IS ?`,
		id, expected.URL, expected.Title, expected.SiteName, expected.Notes, expected.ThumbnailPath, scheduledDateValue(expected.ScheduledDate))
	if err != nil {
		return manga.Work{}, fmt.Errorf("delete work: %w", err)
	}
	count, err := deleted.RowsAffected()
	if err != nil {
		return manga.Work{}, fmt.Errorf("count work deletions: %w", err)
	}
	if count != 1 {
		return manga.Work{}, fmt.Errorf("delete work: expected 1 row, got %d", count)
	}
	commitErr := tx.Commit()
	finalized = true
	if commitErr != nil {
		s.admission.stop()
		return manga.Work{}, fmt.Errorf("commit work delete: %w", errors.Join(ErrWorkDeleteOutcomeUnknown, ErrDBStopped, commitErr))
	}
	committed = true
	return work, nil
}

// Classify only after transaction and connection cleanup. A cleanup error must
// never turn a known Commit into an unknown outcome or authorize image cleanup.
func workDeleteOutcome(work manga.Work, err error, committed bool) (manga.Work, error) {
	if !committed {
		return manga.Work{}, err
	}
	if err != nil {
		err = errors.Join(ErrWorkDeleteCommitted, err)
	}
	return work, err
}
