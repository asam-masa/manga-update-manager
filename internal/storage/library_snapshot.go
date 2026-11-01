package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/asam-masa/manga-update-manager/internal/manga"
)

// LibrarySnapshot is one read view, not a collection of independent API reads.
// Works carry CreatedAt so callers can distinguish reused numeric work IDs.
type LibrarySnapshot struct {
	Works []manga.Work
	Tags  []manga.Tag
	Links []WorkTagLink
}

type WorkTagLink struct {
	WorkID int64
	TagID  int64
}

// LoadLibrarySnapshot reads without changing data, including on an empty DB.
// Keep admission until Rows, the read transaction, and the connection release.
func (s *Store) LoadLibrarySnapshot(ctx context.Context) (result LibrarySnapshot, err error) {
	conn, finish, err := s.begin(ctx)
	if err != nil {
		return LibrarySnapshot{}, err
	}
	defer func() {
		finish(&err)
		if err != nil {
			result = LibrarySnapshot{}
		}
	}()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return LibrarySnapshot{}, fmt.Errorf("begin library read: %w", err)
	}
	// A read-only operation never commits. Rollback also releases its snapshot.
	defer func() {
		if closeErr := tx.Rollback(); closeErr != nil {
			err = errors.Join(err, ErrDBStopped, closeErr)
		}
	}()
	result.Works, err = readLibraryWorks(ctx, tx)
	if err != nil {
		return LibrarySnapshot{}, err
	}
	result.Tags, err = readTags(ctx, tx, "SELECT id,name FROM tags ORDER BY id")
	if err != nil {
		return LibrarySnapshot{}, err
	}
	result.Links, err = readLibraryLinks(ctx, tx, result.Works, result.Tags)
	return result, err
}

func readLibraryWorks(ctx context.Context, q rowQueryer) (result []manga.Work, err error) {
	rows, err := q.QueryContext(ctx, `SELECT id,url,title,site_name,thumbnail_path,notes,
 created_at,updated_at,last_accessed_at,scheduled_date FROM works ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query library works: %w", err)
	}
	defer func() { err = closeLibraryRows(rows, err) }()
	result = make([]manga.Work, 0)
	var previous int64
	for rows.Next() {
		var work manga.Work
		var created, updated string
		var accessed, scheduled sql.NullString
		if err := rows.Scan(&work.ID, &work.URL, &work.Title, &work.SiteName, &work.ThumbnailPath,
			&work.Notes, &created, &updated, &accessed, &scheduled); err != nil {
			return nil, fmt.Errorf("scan library work: %w", err)
		}
		work.CreatedAt, err = parseTime(created)
		if err != nil {
			return nil, err
		}
		work.UpdatedAt, err = parseTime(updated)
		if err != nil {
			return nil, err
		}
		work.LastAccessedAt, err = parseAccessTime(accessed)
		if err != nil {
			return nil, err
		}
		work.ScheduledDate, err = parseScheduledDate(scheduled)
		if err != nil {
			return nil, err
		}
		if work.ID <= previous || work.Validate() != nil ||
			!utf8.ValidString(work.URL) || !utf8.ValidString(work.Title) ||
			!utf8.ValidString(work.SiteName) || !utf8.ValidString(work.ThumbnailPath) || !utf8.ValidString(work.Notes) {
			return nil, errors.New("invalid stored library work")
		}
		previous = work.ID
		result = append(result, work)
	}
	return result, nil
}

func readLibraryLinks(ctx context.Context, q rowQueryer, works []manga.Work, tags []manga.Tag) (result []WorkTagLink, err error) {
	workIDs, tagIDs := make(map[int64]bool, len(works)), make(map[int64]bool, len(tags))
	for _, work := range works {
		workIDs[work.ID] = true
	}
	var previousTag int64
	tagNames := make(map[manga.TagName]bool, len(tags))
	for _, tag := range tags {
		if tag.ID <= previousTag || tagNames[tag.Name] { // Reject duplicates, not repair.
			return nil, errors.New("invalid stored library tags")
		}
		previousTag = tag.ID
		tagIDs[tag.ID] = true
		tagNames[tag.Name] = true
	}
	// Read links directly so missing parents cannot disappear through INNER JOIN.
	rows, err := q.QueryContext(ctx, "SELECT work_id,tag_id FROM work_tags ORDER BY work_id,tag_id")
	if err != nil {
		return nil, fmt.Errorf("query library links: %w", err)
	}
	defer func() { err = closeLibraryRows(rows, err) }()
	result = make([]WorkTagLink, 0)
	var previous WorkTagLink
	for rows.Next() {
		var link WorkTagLink
		if err := rows.Scan(&link.WorkID, &link.TagID); err != nil {
			return nil, fmt.Errorf("scan library link: %w", err)
		}
		if !workIDs[link.WorkID] || !tagIDs[link.TagID] || link.WorkID < previous.WorkID ||
			(link.WorkID == previous.WorkID && link.TagID <= previous.TagID) {
			return nil, errors.New("invalid stored library link")
		}
		previous = link
		result = append(result, link)
	}
	return result, nil
}

func closeLibraryRows(rows *sql.Rows, err error) error {
	// Next can close Rows itself. Preserve both iteration and explicit Close errors.
	if nextErr := rows.Err(); nextErr != nil {
		err = errors.Join(err, ErrDBStopped, nextErr)
	}
	if closeErr := rows.Close(); closeErr != nil {
		err = errors.Join(err, ErrDBStopped, closeErr)
	}
	return err
}
