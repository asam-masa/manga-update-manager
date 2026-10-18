package storage

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

var ErrManagedCoverChanged = errors.New("managed cover is no longer the expected unused record")
var ErrUncertainCoverReferences = errors.New("unrecognized image reference prevents safe cleanup")

// UnusedManagedCover requires the caller's shared reference/cleanup exclusion.
// A record is not file-deletion authority; the caller must verify the real file.
func (s *Store) UnusedManagedCover(ctx context.Context, id string) (_ ManagedCover, unused bool, err error) {
	if !lowerHex(id, 32) {
		return ManagedCover{}, false, ErrInvalidManagedCover
	}
	conn, finish, err := s.begin(ctx)
	if err != nil {
		return ManagedCover{}, false, err
	}
	defer finish(&err)
	cover, err := scanManagedCover(queryOne(ctx, conn, managedCoverByIDQuery, id), id)
	if err != nil {
		return ManagedCover{}, false, err
	}
	if cover.State != "ready" || cover.Info == nil {
		return ManagedCover{}, false, ErrInvalidManagedCover
	}
	referenced, err := coverReferenced(ctx, conn, cover.RelativePath)
	if err != nil {
		return ManagedCover{}, false, err
	}
	return cover, !referenced, nil
}

// ForgetUnusedManagedCover is only for a verified-absent file. Keep the same
// reference exclusion from candidate lookup through this call. The conditional
// DELETE rechecks identity and all references; it never deletes image files.
func (s *Store) ForgetUnusedManagedCover(ctx context.Context, cover ManagedCover) (err error) {
	if !lowerHex(cover.ID, 32) || cover.RelativePath != "covers/"+cover.ID+".png" || cover.State != "ready" || cover.Info == nil || cover.Info.ByteSize <= 0 {
		return ErrInvalidManagedCover
	}
	conn, finish, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer finish(&err)
	referenced, err := coverReferenced(ctx, conn, cover.RelativePath)
	if err != nil {
		return err
	}
	if referenced {
		return ErrManagedCoverChanged
	}
	info := cover.Info
	result, err := conn.ExecContext(ctx, `DELETE FROM managed_covers
 WHERE id=? AND state='ready' AND volume_serial=? AND file_id=? AND byte_size=? AND sha256=?
 AND NOT EXISTS(SELECT 1 FROM works
 WHERE lower(replace(thumbnail_path,char(92),'/'))=managed_covers.relative_path)`,
		cover.ID, fmt.Sprintf("%016x", info.VolumeSerial), hex.EncodeToString(info.FileID[:]), info.ByteSize, info.SHA256[:])
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrManagedCoverChanged
	}
	return nil
}

// Windows paths are not SQLite BINARY strings. Recognize only direct managed
// references (including ASCII case/separator variants). Any other stored path
// may be a Windows alias, so conservatively refuse cleanup without rewriting it.
func coverReferenced(ctx context.Context, conn *sql.Conn, path string) (referenced bool, err error) {
	rows, err := conn.QueryContext(ctx, "SELECT thumbnail_path FROM works WHERE thumbnail_path<>''")
	if err != nil {
		return false, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			err = errors.Join(err, ErrDBStopped, closeErr)
		}
	}()
	uncertain := false
	for rows.Next() {
		var reference string
		if err := rows.Scan(&reference); err != nil {
			return false, err
		}
		normalized := strings.ToLower(strings.ReplaceAll(reference, `\`, "/"))
		id := strings.TrimSuffix(strings.TrimPrefix(normalized, "covers/"), ".png")
		if !lowerHex(id, 32) || normalized != "covers/"+id+".png" {
			uncertain = true
			continue
		}
		if normalized == path {
			referenced = true
		}
	}
	if err := rows.Err(); err != nil {
		return false, errors.Join(ErrDBStopped, err)
	}
	if !referenced && uncertain {
		return false, ErrUncertainCoverReferences
	}
	return referenced, nil
}
