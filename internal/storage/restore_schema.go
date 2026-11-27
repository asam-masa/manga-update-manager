package storage

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"path"
)

// RestoreSchemaQuery uses BINARY order and omits the physical root page.
const RestoreSchemaQuery = `SELECT type, name, tbl_name, sql FROM main.sqlite_schema
ORDER BY type COLLATE BINARY, name COLLATE BINARY, tbl_name COLLATE BINARY`

type RestoreSchemaObject struct {
	Type, Name, Table string
	SQL               sql.NullString
}

// CurrentRestoreSchema builds an isolated reference using only embedded SQL.
func CurrentRestoreSchema(ctx context.Context) (versions []string, objects []RestoreSchemaObject, err error) {
	files, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return nil, nil, err
	}
	if len(files) == 0 {
		return nil, nil, errors.New("no embedded migrations")
	}
	for _, file := range files {
		versions = append(versions, path.Base(file))
	}
	db, err := sql.Open(sqliteDriverName, ":memory:")
	if err != nil {
		return nil, nil, err
	}
	db.SetMaxOpenConns(1)
	defer func() {
		err = errors.Join(err, db.Close(), ctx.Err())
		if err != nil {
			versions, objects = nil, nil
		}
	}()
	if err := migrate(ctx, db); err != nil {
		return nil, nil, err
	}
	rows, err := db.QueryContext(ctx, RestoreSchemaQuery)
	if err != nil {
		return nil, nil, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	for rows.Next() {
		var object RestoreSchemaObject
		if err := rows.Scan(&object.Type, &object.Name, &object.Table, &object.SQL); err != nil {
			return nil, nil, err
		}
		objects = append(objects, object)
	}
	return versions, objects, rows.Err()
}
