package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

var ErrWorkNotFound = errors.New("作品が見つかりません")

const storageTimeFormat = "2006-01-02T15:04:05.000000000Z07:00"

// Store persists application data in SQLite.
type Store struct {
	db *sql.DB
}

// Open opens a SQLite database and applies pending migrations.
func Open(ctx context.Context, dataSourceName string) (*Store, error) {
	if strings.TrimSpace(dataSourceName) == "" {
		return nil, errors.New("SQLiteの保存先を指定してください")
	}

	db, err := sql.Open("sqlite", dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("open SQLite: %w", err)
	}
	db.SetMaxOpenConns(1)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect SQLite: %w", err)
	}
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close releases the SQLite connection pool.
func (s *Store) Close() error {
	return s.db.Close()
}

// CreateWork inserts a new work and returns it with the assigned ID.
func (s *Store) CreateWork(ctx context.Context, work manga.Work) (manga.Work, error) {
	if work.ID != 0 {
		return manga.Work{}, errors.New("登録前の作品にIDを指定できません")
	}
	if err := work.Validate(); err != nil {
		return manga.Work{}, err
	}

	result, err := s.db.ExecContext(ctx, `
INSERT INTO works(url, title, site_name, thumbnail_path, notes, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		work.URL,
		work.Title,
		work.SiteName,
		work.ThumbnailPath,
		work.Notes,
		formatTime(work.CreatedAt),
		formatTime(work.UpdatedAt),
	)
	if err != nil {
		var sqliteErr *sqlite.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
			return manga.Work{}, manga.ErrDuplicateURL
		}
		return manga.Work{}, fmt.Errorf("insert work: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return manga.Work{}, fmt.Errorf("read inserted work ID: %w", err)
	}
	work.ID = id
	return work, nil
}

// WorkByID returns one work by its internal ID.
func (s *Store) WorkByID(ctx context.Context, id int64) (manga.Work, error) {
	var work manga.Work
	var createdAt string
	var updatedAt string

	err := s.db.QueryRowContext(ctx, `
SELECT id, url, title, site_name, thumbnail_path, notes, created_at, updated_at
FROM works
WHERE id = ?`, id).Scan(
		&work.ID,
		&work.URL,
		&work.Title,
		&work.SiteName,
		&work.ThumbnailPath,
		&work.Notes,
		&createdAt,
		&updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return manga.Work{}, ErrWorkNotFound
	}
	if err != nil {
		return manga.Work{}, fmt.Errorf("select work: %w", err)
	}

	work.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return manga.Work{}, fmt.Errorf("parse created_at: %w", err)
	}
	work.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return manga.Work{}, fmt.Errorf("parse updated_at: %w", err)
	}
	return work, nil
}

func formatTime(value time.Time) string {
	return value.Round(0).UTC().Format(storageTimeFormat)
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}
