package storage

import (
	"context"
	"database/sql"
	"errors"
)

var ErrDatabaseCopyCovers = errors.New("database copy cover inventory failed")

// DatabaseCopyCovers reads referenced ready records from an already validated
// current-format copy. The caller retains file identity and parents throughout.
// Records are observations, not permission to read/delete images or restore DBs.
// maxImages is supplied by the caller's format policy; excess is never truncated.
func DatabaseCopyCovers(ctx context.Context, path string, maxImages int) (covers []ManagedCover, err error) {
	defer func() {
		if err != nil {
			covers = nil
			err = errors.Join(ErrDatabaseCopyCovers, err)
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Leave room for the single excess row without overflowing the SQL limit.
	if maxImages < 0 || maxImages == int(^uint(0)>>1) {
		return nil, ErrDatabaseCopyCovers
	}
	dsn, err := databaseCopyReadOnlyDSN(path)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(sqliteDriverName, dsn)
	if err != nil {
		return nil, err
	}
	return readDatabaseCopyCovers(ctx, db, maxImages)
}

func readDatabaseCopyCovers(ctx context.Context, db *sql.DB, maxImages int) (covers []ManagedCover, err error) {
	defer func() {
		err = errors.Join(err, databaseCopyReleaseError(db.Close()), ctx.Err())
		if err != nil {
			covers = nil
		}
	}()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, databaseCopyReleaseError(tx.Rollback()), ctx.Err()) }()
	return referencedCopyCovers(ctx, tx, maxImages)
}

func referencedCopyCovers(ctx context.Context, q rowQueryer, maxImages int) (covers []ManagedCover, err error) {
	// LEFT JOIN makes missing records fail scanning instead of silently dropping
	// a referenced image. DISTINCT deduplicates shared paths before the limit.
	rows, err := q.QueryContext(ctx, `SELECT c.id,c.relative_path,c.state,c.volume_serial,c.file_id,c.byte_size,c.sha256
 FROM (SELECT DISTINCT thumbnail_path FROM works WHERE thumbnail_path<>'' ORDER BY thumbnail_path LIMIT ?) AS refs
 LEFT JOIN managed_covers AS c ON c.relative_path=refs.thumbnail_path ORDER BY refs.thumbnail_path`, maxImages+1)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, databaseCopyReleaseError(rows.Close()), ctx.Err())
		if err != nil {
			covers = nil
		}
	}()
	covers = make([]ManagedCover, 0)
	for rows.Next() {
		if len(covers) >= maxImages {
			return nil, ErrDatabaseCopyCovers
		}
		cover, err := scanManagedCoverRecord(rows)
		if err != nil {
			return nil, err
		}
		if cover.State != "ready" || cover.Info == nil {
			return nil, ErrInvalidManagedCover
		}
		covers = append(covers, cover)
	}
	return covers, databaseCopyReleaseError(rows.Err())
}
