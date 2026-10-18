package storage

import (
	"context"
	"database/sql"
	"errors"
)

// Conn and Tx supply exactly the query operation needed by this consumer.
type rowQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type oneRow struct {
	rows *sql.Rows
	err  error
}

// Unlike QueryRow, keep query rejection separate from row cleanup failures.
func queryOne(ctx context.Context, q rowQueryer, query string, args ...any) oneRow {
	rows, err := q.QueryContext(ctx, query, args...)
	return oneRow{rows: rows, err: err}
}

func (r oneRow) Scan(dest ...any) (err error) {
	if r.err != nil {
		// No acquired Rows to release.
		return r.err
	}
	defer func() {
		if closeErr := r.rows.Close(); closeErr != nil {
			err = errors.Join(err, ErrDBStopped, closeErr)
		}
	}()
	if !r.rows.Next() {
		if nextErr := r.rows.Err(); nextErr != nil {
			// Next can automatically close Rows; its error may be Close failure.
			return errors.Join(ErrDBStopped, nextErr)
		}
		return sql.ErrNoRows
	}
	// Conversion failure is ordinary when the explicit Close succeeds.
	return r.rows.Scan(dest...)
}
