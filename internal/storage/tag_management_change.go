package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

var (
	ErrTagChanged              = errors.New("tag confirmation changed")
	ErrTagDuplicateName        = errors.New("tag name already exists")
	ErrTagConfirmation         = errors.New("invalid tag confirmation")
	ErrTagChangeOutcomeUnknown = errors.New("tag change outcome unknown")
	ErrTagChangeCommitted      = errors.New("tag change committed with cleanup failure")
)

// RenameTag preserves all associations and work metadata. Only the old tag name
// is a confirmation condition; changes to associated works do not reject it.
func (s *Store) RenameTag(ctx context.Context, expected manga.Tag, value string) (manga.Tag, error) {
	name, err := manga.NormalizeTagName(value)
	if err != nil {
		return manga.Tag{}, err
	}
	return s.changeTag(ctx, expected, func(tx *sql.Tx, current manga.Tag) (manga.Tag, bool, error) {
		if current.Name == name {
			return current, false, nil
		}
		if _, err := execTagChange(ctx, tx, "UPDATE tags SET name=? WHERE id=? AND name=?", name, current.ID, expected.Name); err != nil {
			var cause *sqlite.Error
			if errors.As(err, &cause) && cause.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
				return manga.Tag{}, false, ErrTagDuplicateName
			}
			return manga.Tag{}, false, fmt.Errorf("rename tag: %w", err)
		}
		return manga.Tag{ID: current.ID, Name: name}, true, nil
	})
}

// DeleteTag removes only the tag and its FK-cascaded links, never works/images.
// A non-nil empty Works slice explicitly confirms an unused tag.
func (s *Store) DeleteTag(ctx context.Context, expected TagDeletePreview) (manga.Tag, error) {
	if expected.Works == nil {
		return manga.Tag{}, ErrTagConfirmation
	}
	identities := make(map[int64]time.Time, len(expected.Works))
	for _, work := range expected.Works {
		if _, duplicate := identities[work.ID]; duplicate || work.ID <= 0 || work.CreatedAt.IsZero() {
			return manga.Tag{}, ErrTagConfirmation
		}
		identities[work.ID] = work.CreatedAt
	}
	return s.changeTag(ctx, expected.Tag, func(tx *sql.Tx, current manga.Tag) (manga.Tag, bool, error) {
		preview, err := readTagDeletePreview(ctx, tx, current.ID)
		if err != nil {
			return manga.Tag{}, false, err
		}
		if len(preview.Works) != len(identities) {
			return manga.Tag{}, false, ErrTagChanged
		}
		for _, work := range preview.Works {
			at, found := identities[work.ID]
			if !found || !at.Equal(work.CreatedAt) {
				return manga.Tag{}, false, ErrTagChanged
			}
		}
		if _, err := execTagChange(ctx, tx, "DELETE FROM tags WHERE id=? AND name=?", current.ID, expected.Tag.Name); err != nil {
			return manga.Tag{}, false, fmt.Errorf("delete tag: %w", err)
		}
		return current, true, nil
	})
}

// Both management mutations share the same admission, confirmation, transaction,
// and outcome boundary. The callback performs SQL only inside this transaction.
func (s *Store) changeTag(ctx context.Context, expected manga.Tag, change func(*sql.Tx, manga.Tag) (manga.Tag, bool, error)) (result manga.Tag, err error) {
	if expected.ID <= 0 || expected.Name.Validate() != nil {
		return manga.Tag{}, ErrTagConfirmation
	}
	conn, finish, err := s.begin(ctx)
	if err != nil {
		return manga.Tag{}, err
	}
	committed := false
	defer func() {
		finish(&err)
		result, err = tagChangeOutcome(result, err, committed)
	}()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return manga.Tag{}, fmt.Errorf("begin tag change: %w", err)
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
	current, err := tagBy(ctx, tx, "id", expected.ID)
	if errors.Is(err, sql.ErrNoRows) && !errors.Is(err, ErrDBStopped) {
		return manga.Tag{}, ErrTagNotFound
	}
	if err != nil {
		return manga.Tag{}, err
	}
	if current.Name != expected.Name {
		return manga.Tag{}, ErrTagChanged
	}
	result, changed, err := change(tx, current)
	if err != nil || !changed {
		return result, err // Read-only/no-op transactions use Rollback, not Commit.
	}
	commitErr := tx.Commit()
	finalized = true
	if commitErr != nil {
		s.admission.stop()
		return manga.Tag{}, errors.Join(ErrTagChangeOutcomeUnknown, ErrDBStopped, commitErr)
	}
	committed = true
	return result, nil
}

func tagChangeOutcome(tag manga.Tag, err error, committed bool) (manga.Tag, error) {
	if committed && err != nil {
		return tag, errors.Join(ErrTagChangeCommitted, err)
	}
	if err != nil {
		return manga.Tag{}, err
	}
	return tag, nil
}
