package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var ErrInvalidDatabaseCopyTarget = errors.New("database copy target is empty")

// CopyDatabase copies SQLite only. The internal caller must prepare, verify and
// retain a dedicated empty output and parents, drain application operations, and
// verify/finalize the output after this method returns. No filesystem ownership
// or deletion is performed here. Failure may leave a partial output to preserve.
func (s *Store) CopyDatabase(ctx context.Context, destination string) (err error) {
	if strings.TrimSpace(destination) == "" {
		return ErrInvalidDatabaseCopyTarget
	}
	conn, finish, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer finish(&err)
	// VACUUM manages its own snapshot/transaction; do not wrap it in BeginTx.
	if _, err = conn.ExecContext(ctx, "VACUUM INTO ?", destination); err != nil {
		return fmt.Errorf("copy SQLite database: %w", err)
	}
	return nil
}
