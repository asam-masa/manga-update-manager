package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/asam-masa/manga-update-manager/internal/manga"
)

var ErrWorkTagsChanged = errors.New("selected tags changed")

// ListTags includes unused tags. Candidate search belongs to the caller.
func (s *Store) ListTags(ctx context.Context) (result []manga.Tag, err error) {
	conn, finish, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		finish(&err)
		if err != nil {
			result = nil
		}
	}()
	return readTags(ctx, conn, "SELECT id,name FROM tags ORDER BY id")
}

// WorkTags returns ID-ordered snapshots, separately from the comparable Work.
func (s *Store) WorkTags(ctx context.Context, id int64) (result []manga.Tag, err error) {
	conn, finish, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		finish(&err)
		if err != nil {
			result = nil
		}
	}()
	var found int64
	if err := queryOne(ctx, conn, "SELECT id FROM works WHERE id=?", id).Scan(&found); err != nil {
		if errors.Is(err, sql.ErrNoRows) && !errors.Is(err, ErrDBStopped) {
			return nil, ErrWorkNotFound
		}
		return nil, err
	}
	return readTags(ctx, conn, workTagsQuery, id)
}

// LEFT JOIN makes orphaned links fail scanning instead of silently hiding them.
const workTagsQuery = `SELECT t.id,t.name FROM work_tags wt
 LEFT JOIN tags t ON t.id=wt.tag_id WHERE wt.work_id=? ORDER BY t.id`

func readTags(ctx context.Context, q rowQueryer, query string, args ...any) (result []manga.Tag, err error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query tags: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			err = errors.Join(err, ErrDBStopped, closeErr)
		}
		if err != nil {
			result = nil
		}
	}()
	result = make([]manga.Tag, 0)
	for rows.Next() {
		var tag manga.Tag
		if err := rows.Scan(&tag.ID, &tag.Name); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		if tag.ID <= 0 || tag.Name.Validate() != nil {
			return nil, errors.New("invalid stored tag")
		}
		result = append(result, tag)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Join(ErrDBStopped, err)
	}
	return result, nil
}

func tagBy(ctx context.Context, tx *sql.Tx, predicate string, value any) (manga.Tag, error) {
	var tag manga.Tag
	if err := queryOne(ctx, tx, "SELECT id,name FROM tags WHERE "+predicate+"=?", value).Scan(&tag.ID, &tag.Name); err != nil {
		return manga.Tag{}, err
	}
	if tag.ID <= 0 || tag.Name.Validate() != nil {
		return manga.Tag{}, errors.New("invalid stored tag")
	}
	return tag, nil
}

// syncWorkTags runs only inside EditWork's transaction. New candidates and
// associations roll back with a failed pre-commit edit. Commit and cleanup
// outcomes are classified by EditWork, including confirmed commits with warnings.
func syncWorkTags(ctx context.Context, tx *sql.Tx, id int64, input manga.TagEdit) (bool, error) {
	current, err := readTags(ctx, tx, workTagsQuery, id)
	if err != nil {
		return false, err
	}
	wanted := make(map[int64]bool)
	for _, expected := range input.Existing {
		tag, err := tagBy(ctx, tx, "id", expected.ID)
		if errors.Is(err, sql.ErrNoRows) && !errors.Is(err, ErrDBStopped) {
			return false, ErrWorkTagsChanged
		}
		if err != nil {
			return false, err
		}
		if tag.Name != expected.Name {
			return false, ErrWorkTagsChanged
		}
		wanted[tag.ID] = true
	}
	for _, name := range input.NewNames {
		tag, err := tagBy(ctx, tx, "name", name)
		if errors.Is(err, sql.ErrNoRows) && !errors.Is(err, ErrDBStopped) {
			inserted, insertErr := execTagChange(ctx, tx, "INSERT INTO tags(name) VALUES (?)", name)
			if insertErr != nil {
				return false, fmt.Errorf("create tag: %w", insertErr)
			}
			tag.ID, err = inserted.LastInsertId()
		}
		if err != nil {
			return false, err
		}
		wanted[tag.ID] = true
	}
	changed := false
	for _, tag := range current {
		if wanted[tag.ID] {
			delete(wanted, tag.ID)
			continue
		}
		if _, err := execTagChange(ctx, tx, "DELETE FROM work_tags WHERE work_id=? AND tag_id=?", id, tag.ID); err != nil {
			return false, fmt.Errorf("remove work tag: %w", err)
		}
		changed = true
	}
	for tagID := range wanted {
		if _, err := execTagChange(ctx, tx, "INSERT INTO work_tags(work_id,tag_id) VALUES (?,?)", id, tagID); err != nil {
			return false, fmt.Errorf("attach work tag: %w", err)
		}
		changed = true
	}
	return changed, nil
}

func execTagChange(ctx context.Context, tx *sql.Tx, query string, args ...any) (sql.Result, error) {
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if count != 1 {
		return nil, fmt.Errorf("tag change: expected 1 row, got %d", count)
	}
	return result, nil
}

// Equal sets of exact ID/name snapshots; neither order nor timestamps are tokens.
func sameTags(current, expected []manga.Tag) bool {
	if len(current) != len(expected) {
		return false
	}
	seen := make(map[int64]manga.Tag, len(current))
	for _, tag := range current {
		seen[tag.ID] = tag
	}
	for _, tag := range expected {
		if found, ok := seen[tag.ID]; !ok || found != tag {
			return false
		}
		delete(seen, tag.ID)
	}
	return len(seen) == 0
}
