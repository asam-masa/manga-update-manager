package storage

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

var (
	ErrInvalidManagedCover     = errors.New("invalid managed cover record")
	ErrManagedCoverExists      = errors.New("managed cover ID already exists")
	ErrManagedCoverNotReserved = errors.New("managed cover is not reserved")
	ErrManagedCoverNotFound    = errors.New("managed cover record not found")
)

// ManagedCoverInfo holds persisted verification values, not proof of ownership.
// FileID preserves the byte order supplied by the Windows boundary.
type ManagedCoverInfo struct {
	VolumeSerial uint64
	FileID       [16]byte
	ByteSize     int64
	SHA256       [32]byte
}

// ManagedCover is a storage record. Info is nil only for a reserved record.
type ManagedCover struct {
	ID           string
	RelativePath string
	State        string
	Info         *ManagedCoverInfo
}

func lowerHex(value string, width int) bool {
	if len(value) != width {
		return false
	}
	for _, b := range []byte(value) {
		if (b < '0' || b > '9') && (b < 'a' || b > 'f') {
			return false
		}
	}
	return true
}

// ReserveManagedCover reserves a generated ID without enrolling existing files.
func (s *Store) ReserveManagedCover(ctx context.Context, id string) error {
	if !lowerHex(id, 32) {
		return ErrInvalidManagedCover
	}
	_, err := s.db.ExecContext(ctx, "INSERT INTO managed_covers(id,state) VALUES (?,'reserved')", id)
	if err != nil {
		var sqliteErr *sqlite.Error
		if errors.As(err, &sqliteErr) && (sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY || sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE) {
			return ErrManagedCoverExists
		}
		return fmt.Errorf("reserve managed cover: %w", err)
	}
	return nil
}

// ConfirmManagedCover persists verification data exactly once, from reserved.
// The caller must finish file verification before calling this operation.
func (s *Store) ConfirmManagedCover(ctx context.Context, id string, info ManagedCoverInfo) error {
	if !lowerHex(id, 32) || info.ByteSize <= 0 {
		return ErrInvalidManagedCover
	}
	result, err := s.db.ExecContext(ctx, `UPDATE managed_covers
 SET state='ready',volume_serial=?,file_id=?,byte_size=?,sha256=?
 WHERE id=? AND state='reserved'
 AND volume_serial IS NULL AND file_id IS NULL AND byte_size IS NULL AND sha256 IS NULL`, fmt.Sprintf("%016x", info.VolumeSerial), hex.EncodeToString(info.FileID[:]), info.ByteSize, info.SHA256[:], id)
	if err != nil {
		return fmt.Errorf("confirm managed cover: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count confirmed managed covers: %w", err)
	}
	if count != 1 {
		return ErrManagedCoverNotReserved
	}
	return nil
}

// ManagedCoverByID validates persisted state before returning any record.
func (s *Store) ManagedCoverByID(ctx context.Context, id string) (ManagedCover, error) {
	if !lowerHex(id, 32) {
		return ManagedCover{}, ErrInvalidManagedCover
	}
	return scanManagedCover(s.db.QueryRowContext(ctx, managedCoverByIDQuery, id), id)
}

const managedCoverByIDQuery = `SELECT id,relative_path,state,volume_serial,file_id,byte_size,sha256
 FROM managed_covers WHERE id=?`

func scanManagedCover(row *sql.Row, id string) (ManagedCover, error) {
	var value ManagedCover
	var volume, fileID sql.NullString
	var size sql.NullInt64
	var hash sql.Null[[]byte]
	err := row.Scan(&value.ID, &value.RelativePath, &value.State, &volume, &fileID, &size, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return ManagedCover{}, ErrManagedCoverNotFound
	}
	if err != nil {
		return ManagedCover{}, fmt.Errorf("read managed cover: %w", err)
	}
	if value.ID != id || value.RelativePath != "covers/"+id+".png" {
		return ManagedCover{}, ErrInvalidManagedCover
	}
	switch value.State {
	case "reserved":
		if volume.Valid || fileID.Valid || size.Valid || hash.Valid {
			return ManagedCover{}, ErrInvalidManagedCover
		}
	case "ready":
		if !volume.Valid || !fileID.Valid || !size.Valid || !hash.Valid || !lowerHex(volume.String, 16) || !lowerHex(fileID.String, 32) || size.Int64 <= 0 || len(hash.V) != 32 {
			return ManagedCover{}, ErrInvalidManagedCover
		}
		serial, err := strconv.ParseUint(volume.String, 16, 64)
		if err != nil {
			return ManagedCover{}, fmt.Errorf("decode cover volume: %w", err)
		}
		decoded, err := hex.DecodeString(fileID.String)
		if err != nil {
			return ManagedCover{}, fmt.Errorf("decode cover ID: %w", err)
		}
		info := &ManagedCoverInfo{VolumeSerial: serial, ByteSize: size.Int64}
		copy(info.FileID[:], decoded)
		copy(info.SHA256[:], hash.V)
		value.Info = info
	default:
		return ManagedCover{}, ErrInvalidManagedCover
	}
	return value, nil
}
