package storage

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/asam-masa/manga-update-manager/internal/manga"
)

// ListWorks returns all works in ascending ID order, including an empty slice.
func (s *Store) ListWorks(ctx context.Context) ([]manga.Work, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, url, title, site_name, thumbnail_path, notes, created_at, updated_at, last_accessed_at FROM works ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list works: %w", err)
	}
	defer rows.Close()
	works := make([]manga.Work, 0)
	for rows.Next() {
		var work manga.Work
		var created, updated string
		var accessed sql.NullString
		if err := rows.Scan(&work.ID, &work.URL, &work.Title, &work.SiteName, &work.ThumbnailPath, &work.Notes, &created, &updated, &accessed); err != nil {
			return nil, fmt.Errorf("scan work: %w", err)
		}
		work.CreatedAt, err = parseTime(created)
		if err != nil {
			return nil, fmt.Errorf("parse created_at: %w", err)
		}
		work.UpdatedAt, err = parseTime(updated)
		if err != nil {
			return nil, fmt.Errorf("parse updated_at: %w", err)
		}
		work.LastAccessedAt, err = parseAccessTime(accessed)
		if err != nil {
			return nil, err
		}
		works = append(works, work)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate works: %w", err)
	}
	return works, nil
}
