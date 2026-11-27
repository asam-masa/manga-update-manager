package application

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/asam-masa/manga-update-manager/internal/storage"
)

var (
	errRestoreMigrationHistory = errors.New("restore migration history does not match current version")
	errRestoreSchema           = errors.New("restore database structure does not match current version")
)

func checkPreparedRestoreSchema(ctx context.Context, p *PreparedRestore) (restoreSQLResult, error) {
	versions, objects, err := storage.CurrentRestoreSchema(ctx)
	if err != nil {
		return restoreSQLResult{OperationErr: err, ContextErr: ctx.Err()}, err
	}
	if !slices.Equal(p.manifest.Migrations, versions) {
		return restoreSQLResult{OperationErr: errRestoreMigrationHistory}, errRestoreMigrationHistory
	}
	ops := preparedRestoreSQLOps()
	ops.check = func(ctx context.Context, s *restoreSQLSession, r *restoreSQLResult) {
		maxName := 0
		for _, version := range versions {
			maxName = max(maxName, len(version))
		}
		historyQuery := `SELECT CASE WHEN typeof(version)='text' AND length(CAST(version AS BLOB))<=? THEN version END
FROM main.schema_migrations ORDER BY version COLLATE BINARY LIMIT ?`
		historyArgs := []driver.NamedValue{{Ordinal: 1, Value: int64(maxName)}, {Ordinal: 2, Value: int64(len(versions) + 1)}}
		s.compareRows(ctx, r, historyQuery, historyArgs, len(versions), 1, errRestoreMigrationHistory, func(i int, row []driver.Value) bool {
			return row[0] == versions[i]
		})
		if r.OperationErr != nil || r.ReleaseErr != nil || s.failure() != nil || ctx.Err() != nil {
			return
		}
		s.compareRows(ctx, r, storage.RestoreSchemaQuery, nil, len(objects), 4, errRestoreSchema, func(i int, row []driver.Value) bool {
			object := objects[i]
			if row[0] != object.Type || row[1] != object.Name || row[2] != object.Table {
				return false
			}
			if !object.SQL.Valid {
				return row[3] == nil
			}
			return row[3] == object.SQL.String
		})
	}
	return runRestoreSQL(ctx, p, ops)
}

func (s *restoreSQLSession) compareRows(ctx context.Context, r *restoreSQLResult, statement string, args []driver.NamedValue, count, columns int, mismatch error, matches func(int, []driver.Value) bool) {
	if !s.query(ctx, r, statement, args...) {
		return
	}
	defer func() {
		s.rowsCloseAttempted = true
		if err := s.rows.Close(); err != nil {
			r.Rows = restoreSQLUnknown
			r.ReleaseErr = errors.Join(r.ReleaseErr, fmt.Errorf("close restore rows: %w", err))
			return
		}
		s.rows = nil
		r.Rows = restoreSQLReleased
	}()
	values := make([]driver.Value, columns)
	for i := 0; s.failure() == nil && ctx.Err() == nil; i++ {
		err := s.rows.Next(values)
		if s.failure() != nil || ctx.Err() != nil {
			if err != io.EOF {
				r.OperationErr = err
			}
			return
		}
		if err == io.EOF {
			if i != count {
				r.OperationErr = mismatch
			}
			return
		}
		if err != nil {
			r.OperationErr = err
			return
		}
		if i == count || !matches(i, values) {
			r.OperationErr = mismatch
			return
		}
	}
}
