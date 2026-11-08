package storage

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"path"
)

var ErrDatabaseCopyMigrations = errors.New("database copy migration history mismatch or read failure")

// DatabaseCopyMigrations observes the exact current history for backup creation.
// The caller retains the validated copy's identity and parents throughout.
// Matching names do not establish schema compatibility or restoration safety.
// No migration, repair or write is performed, including for old-format copies.
func DatabaseCopyMigrations(ctx context.Context, copyPath string) (versions []string, err error) {
	defer func() {
		if err != nil {
			versions = nil
			err = errors.Join(ErrDatabaseCopyMigrations, err)
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dsn, err := databaseCopyReadOnlyDSN(copyPath)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(sqliteDriverName, dsn)
	if err != nil {
		return nil, err
	}
	return readDatabaseCopyMigrations(ctx, db)
}

func readDatabaseCopyMigrations(ctx context.Context, db *sql.DB) (versions []string, err error) {
	defer func() {
		err = errors.Join(err, databaseCopyReleaseError(db.Close()), ctx.Err())
		if err != nil {
			versions = nil
		}
	}()
	files, err := fs.Glob(migrationFiles, "migrations/*.sql") // Sorted by fs.Glob.
	if err != nil || len(files) == 0 {
		return nil, errors.Join(ErrDatabaseCopyMigrations, err)
	}
	maxName := 0
	for i, file := range files {
		files[i] = path.Base(file)
		if len(files[i]) > maxName {
			maxName = len(files[i])
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, databaseCopyReleaseError(tx.Rollback()), ctx.Err()) }()
	// Bound both rows and values before allocating result strings in Go. NULL
	// intentionally fails Scan for non-text or oversized names, never truncates.
	rows, err := tx.QueryContext(ctx, `SELECT CASE WHEN typeof(version)='text' AND length(CAST(version AS BLOB))<=? THEN version ELSE NULL END
 FROM schema_migrations ORDER BY version COLLATE BINARY LIMIT ?`, maxName, len(files)+1)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, databaseCopyReleaseError(rows.Close()), ctx.Err()) }()
	versions = make([]string, 0, len(files))
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		if len(versions) >= len(files) || version != files[len(versions)] {
			return nil, ErrDatabaseCopyMigrations
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return nil, databaseCopyReleaseError(err)
	}
	if len(versions) != len(files) {
		return nil, ErrDatabaseCopyMigrations
	}
	return versions, nil
}
