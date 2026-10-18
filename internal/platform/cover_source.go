package platform

import (
	"context"
	"errors"
	"io"
	"os"
)

// OpenCoverSource opens the explicitly selected source read-only. The caller
// owns Close and must not persist or expose this path as a managed image name.
func OpenCoverSource(ctx context.Context, path string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = ErrInvalidCoverFile
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return nil, errors.Join(err, closeCoverResource(f))
	}
	return f, nil
}
