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

var ErrWorkNotFound = manga.ErrWorkNotFound

const storageTimeFormat = "2006-01-02T15:04:05.000000000Z07:00"

// Store persists application data in SQLite.
type Store struct {
	db        *sql.DB
	admission dbAdmission
}

// Open opens a SQLite database and applies pending migrations.
func Open(ctx context.Context, dataSourceName string) (*Store, error) {
	if strings.TrimSpace(dataSourceName) == "" {
		return nil, errors.New("SQLiteの保存先を指定してください")
	}

	db, err := sql.Open(sqliteDriverName, dataSourceName)
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
	s.admission.stop()
	s.admission.slot <- struct{}{}
	defer func() { <-s.admission.slot }()
	return s.db.Close()
}

// CreateWork inserts a new work and returns it with the assigned ID.
func (s *Store) CreateWork(ctx context.Context, work manga.Work) (_ manga.Work, err error) {
	if work.ID != 0 {
		return manga.Work{}, errors.New("登録前の作品にIDを指定できません")
	}
	if work.LastAccessedAt != nil {
		return manga.Work{}, errors.New("登録前の作品に最終アクセス日時を指定できません")
	}
	if err := work.Validate(); err != nil {
		return manga.Work{}, err
	}

	conn, finish, err := s.begin(ctx)
	if err != nil {
		return manga.Work{}, err
	}
	defer finish(&err)
	result, err := conn.ExecContext(ctx, `
INSERT INTO works(url, title, site_name, thumbnail_path, notes, created_at, updated_at, scheduled_date)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		work.URL,
		work.Title,
		work.SiteName,
		work.ThumbnailPath,
		work.Notes,
		formatTime(work.CreatedAt),
		formatTime(work.UpdatedAt),
		scheduledDateValue(work.ScheduledDate),
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
func (s *Store) WorkByID(ctx context.Context, id int64) (_ manga.Work, err error) {
	conn, finish, err := s.begin(ctx)
	if err != nil {
		return manga.Work{}, err
	}
	defer finish(&err)
	return scanWork(queryOne(ctx, conn, workByIDQuery, id))
}

const workByIDQuery = `SELECT id, url, title, site_name, thumbnail_path, notes, created_at, updated_at, last_accessed_at, scheduled_date
FROM works WHERE id = ?`

func scanWork(row oneRow) (manga.Work, error) {
	var work manga.Work
	var createdAt string
	var updatedAt string
	var accessed sql.NullString
	var scheduled sql.NullString

	err := row.Scan(
		&work.ID,
		&work.URL,
		&work.Title,
		&work.SiteName,
		&work.ThumbnailPath,
		&work.Notes,
		&createdAt,
		&updatedAt,
		&accessed,
		&scheduled,
	)
	if errors.Is(err, sql.ErrNoRows) && !errors.Is(err, ErrDBStopped) {
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
	work.LastAccessedAt, err = parseAccessTime(accessed)
	if err != nil {
		return manga.Work{}, err
	}
	work.ScheduledDate, err = parseScheduledDate(scheduled)
	if err != nil {
		return manga.Work{}, err
	}
	return work, nil
}

func scheduledDateValue(date manga.ScheduledDate) any {
	if date == "" {
		return nil
	}
	return string(date)
}

func parseScheduledDate(value sql.NullString) (manga.ScheduledDate, error) {
	if !value.Valid {
		return "", nil
	}
	date := manga.ScheduledDate(value.String)
	if date == "" || date.Validate() != nil {
		return "", fmt.Errorf("parse scheduled_date: %w", manga.ErrScheduledDate)
	}
	return date, nil
}

func formatTime(value time.Time) string {
	return value.Round(0).UTC().Format(storageTimeFormat)
}

func parseAccessTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid {
		return nil, nil
	}
	parsed, err := parseTime(value.String)
	if err != nil {
		return nil, fmt.Errorf("parse last_accessed_at: %w", err)
	}
	return &parsed, nil
}

// RecordLastAccess changes access metadata, not the work's content modification date.
func (s *Store) RecordLastAccess(ctx context.Context, id int64, at time.Time) (err error) {
	if at.IsZero() {
		return manga.ErrTimestampInvalid
	}
	conn, finish, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer finish(&err)
	result, err := conn.ExecContext(ctx, "UPDATE works SET last_accessed_at = ? WHERE id = ?", formatTime(at), id)
	if err != nil {
		return fmt.Errorf("record last access: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrWorkNotFound
	}
	return nil
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}
