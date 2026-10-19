package storage

import (
	"context"
	"errors"
	"fmt"
)

// ListManagedCovers returns a fully validated inventory, not deletion authority.
// Reserved and referenced records are included. Callers must separately hold
// reference exclusion and verify all references and the real file before cleanup.
func (s *Store) ListManagedCovers(ctx context.Context) (covers []ManagedCover, err error) {
	conn, finish, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	// Runs after Rows cleanup. Never return usable candidates with a late
	// resource-release error, even if all rows were already read successfully.
	defer func() {
		finish(&err)
		if err != nil {
			covers = nil
		}
	}()
	rows, err := conn.QueryContext(ctx, `SELECT id,relative_path,state,volume_serial,file_id,byte_size,sha256
 FROM managed_covers ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list managed covers: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			err = errors.Join(err, ErrDBStopped, closeErr)
		}
	}()
	covers = make([]ManagedCover, 0)
	for rows.Next() {
		cover, scanErr := scanManagedCoverRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		covers = append(covers, cover)
	}
	if err := rows.Err(); err != nil {
		// Next may already have closed Rows and retained a Close error here.
		return nil, fmt.Errorf("iterate managed covers: %w", errors.Join(ErrDBStopped, err))
	}
	return covers, nil
}
