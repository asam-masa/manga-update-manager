package storage

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
)

var ErrDatabaseCopyValidation = errors.New("database copy validation failed")

// ValidateDatabaseCopy checks an already prepared copy, never the live Store.
// The caller retains its identity and parents; path validation is not ownership.
// No migrations, repairs or writes are performed. Success is not full domain
// validation, schema compatibility or a guarantee about later changes.
func ValidateDatabaseCopy(ctx context.Context, path string) (err error) {
	defer func() {
		if err != nil {
			err = errors.Join(ErrDatabaseCopyValidation, err)
		}
	}()
	if err = ctx.Err(); err != nil {
		return err
	}
	dsn, err := databaseCopyReadOnlyDSN(path)
	if err != nil {
		return err
	}
	db, err := sql.Open(sqliteDriverName, dsn)
	if err != nil {
		return err
	}
	return validateDatabaseCopy(ctx, db)
}

func databaseCopyReadOnlyDSN(path string) (string, error) {
	if !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return "", ErrDatabaseCopyValidation
	}
	p := filepath.ToSlash(path)
	if strings.HasPrefix(p, "//") {
		return "", ErrDatabaseCopyValidation
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	} // Windows drive URI: file:///C:/...
	return (&url.URL{Scheme: "file", Path: p, RawQuery: "mode=ro&cache=private"}).String(), nil
}

func validateDatabaseCopy(ctx context.Context, db *sql.DB) (err error) {
	defer func() { err = errors.Join(err, databaseCopyReleaseError(db.Close()), ctx.Err()) }()
	// mode=ro enforces read-only even though this transaction uses default options.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, databaseCopyReleaseError(tx.Rollback()), ctx.Err())
	}()
	if err = checkCopyIntegrity(ctx, tx); err != nil {
		return err
	}
	return checkCopyForeignKeys(ctx, tx)
}

func checkCopyIntegrity(ctx context.Context, q rowQueryer) (err error) {
	rows, err := q.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, databaseCopyReleaseError(rows.Close())) }()
	count := 0
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return err
		}
		count++
		if count != 1 || value != "ok" {
			return ErrDatabaseCopyValidation
		}
	}
	if err := rows.Err(); err != nil {
		return databaseCopyReleaseError(err)
	}
	if count != 1 {
		return ErrDatabaseCopyValidation
	}
	return nil
}

func checkCopyForeignKeys(ctx context.Context, q rowQueryer) (err error) {
	rows, err := q.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, databaseCopyReleaseError(rows.Close())) }()
	if rows.Next() {
		return ErrDatabaseCopyValidation
	}
	return databaseCopyReleaseError(rows.Err())
}
