package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"

	"modernc.org/sqlite"
)

const sqliteDriverName = "manga-sqlite"

func init() {
	d := &sqlite.Driver{}
	d.RegisterConnectionHook(enableForeignKeys)
	sql.Register(sqliteDriverName, d)
}

// This driver instance affects only Store.Open, not other users of "sqlite".
// The hook runs after DSN pragmas on every new physical connection, before use.
func enableForeignKeys(conn sqlite.ExecQuerierContext, _ string) (err error) {
	ctx := context.Background()
	if _, err = conn.ExecContext(ctx, "PRAGMA foreign_keys=ON", nil); err != nil {
		return fmt.Errorf("enable foreign keys: %w", err)
	}
	rows, err := conn.QueryContext(ctx, "PRAGMA foreign_keys", nil)
	if err != nil {
		return fmt.Errorf("query foreign keys: %w", err)
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	value := make([]driver.Value, 1)
	if err := rows.Next(value); err != nil {
		return fmt.Errorf("read foreign keys: %w", err)
	}
	if value[0] != int64(1) {
		return errors.New("foreign keys are not enabled")
	}
	return nil
}
