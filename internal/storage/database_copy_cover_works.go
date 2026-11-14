package storage

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	MaxCopyCoverWorks      = 50_000
	MaxCopyCoverTitleBytes = 4 * 1024
	MaxCopyCoverTotalBytes = 8 * 1024 * 1024
)

var (
	ErrDatabaseCopyCoverWorks = errors.New("database copy cover work details unavailable")
	ErrCopyCoverWorkLimit     = errors.New("database copy cover work detail limit exceeded")
)

// CopyCoverWork is a snapshot observation, not authority to read/delete files.
type CopyCoverWork struct {
	CoverID string
	WorkID  int64
	Title   string
}

// DatabaseCopyCoverWorks reads all works for the requested omitted ready covers
// from an already validated current-format copy. The caller retains the copy's
// identity and parents until return. Results are ordered by cover ID, then work
// ID; any failure discards all details. Release/cancellation causes remain joined
// to the error and must not be treated as a harmless detail warning.
func DatabaseCopyCoverWorks(ctx context.Context, path string, coverIDs []string) (works []CopyCoverWork, err error) {
	defer func() {
		if err != nil {
			works = nil
			err = errors.Join(ErrDatabaseCopyCoverWorks, err)
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// The format permits at most 10,000 images; bound SQL parameters as well.
	if len(coverIDs) > 10_000 {
		return nil, ErrCopyCoverWorkLimit
	}
	seen := make(map[string]bool, len(coverIDs))
	for _, id := range coverIDs {
		if !lowerHex(id, 32) || seen[id] {
			return nil, ErrDatabaseCopyCoverWorks
		}
		seen[id] = true
	}
	dsn, err := databaseCopyReadOnlyDSN(path)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(sqliteDriverName, dsn)
	if err != nil {
		return nil, err
	}
	return readDatabaseCopyCoverWorks(ctx, db, coverIDs)
}

func readDatabaseCopyCoverWorks(ctx context.Context, db *sql.DB, ids []string) (works []CopyCoverWork, err error) {
	defer func() {
		err = errors.Join(err, databaseCopyReleaseError(db.Close()), ctx.Err())
		if err != nil {
			works = nil
		}
	}()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, databaseCopyReleaseError(tx.Rollback()), ctx.Err()) }()
	return copyCoverWorks(ctx, tx, ids)
}

func copyCoverWorks(ctx context.Context, q rowQueryer, ids []string) (works []CopyCoverWork, err error) {
	works = make([]CopyCoverWork, 0)
	if len(ids) == 0 {
		return works, nil
	}
	args := make([]any, 0, len(ids)+2)
	args = append(args, MaxCopyCoverTitleBytes)
	for _, id := range ids {
		args = append(args, id)
	}
	args = append(args, MaxCopyCoverWorks+1)
	// CASE prevents oversized text reaching Go; BLOB length counts UTF-8 bytes,
	// including embedded NULs. LIMIT fetches one extra row to detect truncation.
	rows, err := q.QueryContext(ctx, `SELECT c.id,w.id,
 CASE WHEN typeof(w.title)='text' THEN length(CAST(w.title AS BLOB)) ELSE -1 END,
 CASE WHEN typeof(w.title)='text' AND length(CAST(w.title AS BLOB))<=? THEN w.title ELSE NULL END
 FROM works AS w JOIN managed_covers AS c ON c.relative_path=w.thumbnail_path
 WHERE c.state='ready' AND c.id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+`)
 ORDER BY c.id,w.id LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, databaseCopyReleaseError(rows.Close()), ctx.Err())
		if err != nil {
			works = nil
		}
	}()
	seen := make(map[string]bool, len(ids))
	total := 0
	for rows.Next() {
		if len(works) >= MaxCopyCoverWorks {
			return nil, ErrCopyCoverWorkLimit
		}
		var work CopyCoverWork
		var size int64
		var title sql.NullString
		if err := rows.Scan(&work.CoverID, &work.WorkID, &size, &title); err != nil {
			return nil, err
		}
		if size > MaxCopyCoverTitleBytes || size > int64(MaxCopyCoverTotalBytes-total) {
			return nil, ErrCopyCoverWorkLimit
		}
		if size <= 0 || !title.Valid || int64(len(title.String)) != size || !utf8.ValidString(title.String) || work.WorkID <= 0 {
			return nil, ErrDatabaseCopyCoverWorks
		}
		work.Title = title.String
		total += len(work.Title)
		seen[work.CoverID] = true
		works = append(works, work)
	}
	if err := rows.Err(); err != nil {
		return nil, databaseCopyReleaseError(err)
	}
	// Requested IDs originate in the referenced ready-cover inventory. A missing
	// match is unavailable information, never a successful zero-work observation.
	if len(seen) != len(ids) {
		return nil, ErrDatabaseCopyCoverWorks
	}
	return works, nil
}
