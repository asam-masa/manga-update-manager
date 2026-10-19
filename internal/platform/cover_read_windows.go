package platform

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// MaxCoverReadBytes bounds each buffered read of a generated cover (600px side).
const MaxCoverReadBytes int64 = 2 * 1024 * 1024

// ReadVerifiedCover returns owned bytes only after verification and release.
// The caller must resolve a ready DB record and retain reference exclusion
// through this call. This boundary neither validates PNG syntax nor changes DB.
func ReadVerifiedCover(ctx context.Context, directory, id string, expected CoverFileInfo) (data []byte, err error) {
	if len(id) != 32 || expected.ByteSize <= 0 || expected.ByteSize > MaxCoverReadBytes {
		return nil, ErrInvalidCoverFile
	}
	for _, b := range []byte(id) {
		if !(b >= '0' && b <= '9') && !(b >= 'a' && b <= 'f') {
			return nil, ErrInvalidCoverFile
		}
	}
	guard, err := holdCoverDirectory(ctx, directory, false)
	if err != nil {
		return nil, err
	}
	var file *os.File
	defer func() {
		if file != nil {
			err = errors.Join(err, closeCoverResource(file))
		}
		err = errors.Join(err, guard.Close())
		if err != nil {
			data = nil
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err = openCoverFile(filepath.Join(directory, "covers", id+".png"), windows.GENERIC_READ, windows.OPEN_EXISTING)
	if err != nil {
		return nil, err
	}
	actual, err := captureCoverFile(file, expected.ByteSize)
	if err != nil {
		return nil, err
	}
	if actual != expected {
		return nil, ErrInvalidCoverFile
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	data, err = io.ReadAll(io.LimitReader(file, expected.ByteSize+1))
	if err != nil {
		return nil, err
	}
	// Verify the actual response, not only the earlier identity/hash capture.
	if int64(len(data)) != expected.ByteSize || sha256.Sum256(data) != expected.SHA256 {
		return nil, ErrInvalidCoverFile
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}
