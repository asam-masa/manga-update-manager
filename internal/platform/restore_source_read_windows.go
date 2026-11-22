package platform

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
)

// Reader borrows this owner's creation handle. Finish every consumer before
// Close; use VerifyIdentity before validation/extraction and after the last read.
// The context is checked around each I/O, not used to force-abort OS calls.
func (s *RestoreSource) Reader(ctx context.Context) io.ReaderAt {
	return restoreSourceReader{source: s, ctx: ctx}
}

type restoreSourceReader struct {
	source *RestoreSource
	ctx    context.Context
}

func (r restoreSourceReader) ReadAt(p []byte, offset int64) (int, error) {
	return r.source.readAt(r.ctx, p, offset, (*os.File).ReadAt)
}

func (s *RestoreSource) readAt(ctx context.Context, p []byte, offset int64, read func(*os.File, []byte, int64) (int, error)) (int, error) {
	if s == nil {
		return 0, os.ErrClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.release.CloseAttempted || s.file == nil {
		return 0, os.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	n, err := read(s.file, p, offset)
	if cancelled := ctx.Err(); cancelled != nil {
		err = errors.Join(err, cancelled)
	}
	// Preserve the exact EOF value when there is no cancellation.
	return n, err
}

// Size rechecks the held input against its acquisition baseline; it is not an
// unchecked cached size. Neither success nor VerifyIdentity proves valid ZIP data.
func (s *RestoreSource) Size(ctx context.Context) (int64, error) {
	return s.inspect(ctx, observeRestoreSource)
}

func (s *RestoreSource) VerifyIdentity(ctx context.Context) error {
	_, err := s.inspect(ctx, observeRestoreSource)
	return err
}

func (s *RestoreSource) inspect(ctx context.Context, observe func(*os.File) (restoreSourceObservation, error)) (int64, error) {
	if s == nil {
		return 0, os.ErrClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.release.CloseAttempted || s.file == nil {
		return 0, os.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	o, err := observe(s.file)
	if err == nil && (o.id != s.identity || !strings.EqualFold(o.name, s.path) || o.size != s.size) {
		err = ErrUnsafeRestoreSource
	}
	if err != nil {
		err = errors.Join(ErrUnsafeRestoreSource, err)
	}
	if err = errors.Join(err, ctx.Err()); err != nil {
		return 0, err
	}
	return o.size, nil
}
