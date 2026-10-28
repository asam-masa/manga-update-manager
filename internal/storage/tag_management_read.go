package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/asam-masa/manga-update-manager/internal/manga"
)

var ErrTagNotFound = errors.New("tag not found")

// TagUsage counts all associated works, independently of list filters.
type TagUsage struct {
	Tag       manga.Tag
	WorkCount int64
}

// TagWork identifies a work separately from its editable display title.
// CreatedAt accompanies ID because works may reuse a deleted numeric ID.
type TagWork struct {
	ID        int64
	CreatedAt time.Time
	Title     string
}

// TagDeletePreview is display/confirmation data, not permission to delete.
// A future delete operation must recheck its conditions inside its transaction.
type TagDeletePreview struct {
	Tag   manga.Tag
	Works []TagWork
}

func (s *Store) ListTagUsage(ctx context.Context) (result []TagUsage, err error) {
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
	// Count links as well as resolved works so orphaned links cannot look unused.
	rows, err := conn.QueryContext(ctx, `SELECT t.id,t.name,COUNT(wt.work_id),COUNT(CASE WHEN w.id>0 THEN w.id END)
 FROM tags t LEFT JOIN work_tags wt ON wt.tag_id=t.id
 LEFT JOIN works w ON w.id=wt.work_id GROUP BY t.id,t.name ORDER BY t.id`)
	if err != nil {
		return nil, fmt.Errorf("query tag usage: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			err = errors.Join(err, ErrDBStopped, closeErr)
		}
	}()
	result = make([]TagUsage, 0)
	for rows.Next() {
		var item TagUsage
		var links int64
		if err := rows.Scan(&item.Tag.ID, &item.Tag.Name, &links, &item.WorkCount); err != nil {
			return nil, fmt.Errorf("scan tag usage: %w", err)
		}
		if item.Tag.ID <= 0 || item.Tag.Name.Validate() != nil || links != item.WorkCount {
			return nil, errors.New("invalid stored tag usage")
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Join(ErrDBStopped, err)
	}
	return result, nil
}

func (s *Store) PreviewTagDelete(ctx context.Context, id int64) (result TagDeletePreview, err error) {
	conn, finish, err := s.begin(ctx)
	if err != nil {
		return TagDeletePreview{}, err
	}
	defer func() {
		finish(&err)
		if err != nil {
			result = TagDeletePreview{}
		}
	}()
	// One statement supplies both the exact tag name and its associated works.
	// A separate name read followed by a target read could mix two DB snapshots.
	rows, err := conn.QueryContext(ctx, `SELECT t.id,t.name,wt.work_id,w.id,w.created_at,w.title
 FROM tags t LEFT JOIN work_tags wt ON wt.tag_id=t.id
 LEFT JOIN works w ON w.id=wt.work_id WHERE t.id=? ORDER BY wt.work_id`, id)
	if err != nil {
		return TagDeletePreview{}, fmt.Errorf("query tag delete preview: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			err = errors.Join(err, ErrDBStopped, closeErr)
		}
	}()
	result.Works = make([]TagWork, 0)
	for rows.Next() {
		var tag manga.Tag
		var linkID, workID sql.NullInt64
		var created, title sql.NullString
		if err := rows.Scan(&tag.ID, &tag.Name, &linkID, &workID, &created, &title); err != nil {
			return TagDeletePreview{}, fmt.Errorf("scan tag delete preview: %w", err)
		}
		if tag.ID <= 0 || tag.Name.Validate() != nil {
			return TagDeletePreview{}, errors.New("invalid stored tag")
		}
		result.Tag = tag
		if !linkID.Valid {
			continue // Existing unused tag; distinct from no tag row at all.
		}
		if !workID.Valid || workID.Int64 <= 0 || !created.Valid || !title.Valid || strings.TrimSpace(title.String) == "" || !utf8.ValidString(title.String) {
			return TagDeletePreview{}, errors.New("invalid stored tag work")
		}
		at, err := parseTime(created.String)
		if err != nil || at.IsZero() {
			return TagDeletePreview{}, errors.New("invalid stored tag work creation time")
		}
		result.Works = append(result.Works, TagWork{ID: workID.Int64, CreatedAt: at, Title: title.String})
	}
	if err := rows.Err(); err != nil {
		return TagDeletePreview{}, errors.Join(ErrDBStopped, err)
	}
	if result.Tag.ID == 0 {
		return TagDeletePreview{}, ErrTagNotFound
	}
	return result, nil
}
