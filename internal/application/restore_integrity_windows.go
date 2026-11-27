package application

import (
	"context"
	"database/sql/driver"
	"errors"
)

var (
	errRestoreIntegrity   = errors.New("restore SQLite integrity check failed")
	errRestoreForeignKeys = errors.New("restore SQLite foreign key check failed")
)

func (s *restoreSQLSession) checkIntegrity(ctx context.Context, r *restoreSQLResult) {
	s.compareRows(ctx, r, "PRAGMA main.integrity_check", nil, 1, 1, errRestoreIntegrity, func(_ int, row []driver.Value) bool {
		return row[0] == "ok"
	})
	// SQLite reports one row per violation, with four columns. No values
	// are needed to reject a violation or exposed in the returned error.
	s.compareRows(ctx, r, "PRAGMA main.foreign_key_check", nil, 0, 4, errRestoreForeignKeys, nil)
}
