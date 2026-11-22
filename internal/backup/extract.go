package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
)

var ErrArchiveExtract = errors.New("backup archive extraction failed")

// ExtractArchive revalidates the entire ZIP against a previously validated
// manifest and streams DB/images into borrowed writers, DB first. File.Path is
// a container name, never a filesystem destination. The caller retains the
// stable input, maps each File to its own newly created output, and owns every
// writer even when open returns both a writer and an error. Nothing is closed,
// synced, deleted or published here. On any error ALL output must be discarded
// through its owner's cleanup; bytes already written are not a partial success.
// Callers must not mutate expected during this call. Cancellation is checked
// around I/O/callbacks; a blocked OS call cannot be forcibly interrupted.
func ExtractArchive(ctx context.Context, r io.ReaderAt, size int64, expected Manifest, open func(File) (io.Writer, error)) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %w", ErrArchiveExtract, err)
	}
	if r == nil || open == nil {
		return fmt.Errorf("%w: %w", ErrArchiveExtract, ErrArchive)
	}
	input := &extractionInput{ctx: ctx, src: r}
	x := &archiveExtraction{ctx: ctx, expected: expected, open: open, input: input}
	_, err := validateArchive(input, size, x)
	if err = errors.Join(err, input.readErr, ctx.Err()); err != nil {
		return fmt.Errorf("%w: %w", ErrArchiveExtract, err)
	}
	return nil
}

type archiveExtraction struct {
	ctx      context.Context
	expected Manifest
	open     func(File) (io.Writer, error)
	input    *extractionInput
}

func (x *archiveExtraction) destination(file File) (io.Writer, error) {
	if err := errors.Join(x.ctx.Err(), x.input.readErr); err != nil {
		return nil, err
	}
	w, err := x.open(file)
	if err = errors.Join(err, x.ctx.Err()); err != nil {
		return nil, err // The caller still owns any output created by open.
	}
	if w == nil {
		return nil, errors.New("missing extraction writer")
	}
	return &extractionOutput{ctx: x.ctx, dst: w, remaining: file.Size, input: x.input}, nil
}

func sameManifest(a, b Manifest) bool {
	return a.FormatVersion == b.FormatVersion && a.CreatedAt == b.CreatedAt &&
		a.AppVersion == b.AppVersion && a.Database == b.Database &&
		slices.Equal(a.Migrations, b.Migrations) && slices.Equal(a.Images, b.Images) &&
		slices.Equal(a.OmittedImages, b.OmittedImages)
}

type extractionInput struct {
	ctx     context.Context
	src     io.ReaderAt
	readErr error
}

func (r *extractionInput) ReadAt(p []byte, off int64) (int, error) {
	if r.readErr != nil {
		return 0, r.readErr
	}
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.src.ReadAt(p, off)
	// archive/zip may accept returned bytes despite a non-EOF ReaderAt error.
	// Latch that cause so it cannot become a successful extraction or new output.
	if err != nil && err != io.EOF {
		r.readErr = err
	}
	if cancelled := r.ctx.Err(); cancelled != nil {
		err = errors.Join(err, cancelled)
	}
	return n, err
}

type extractionOutput struct {
	ctx       context.Context
	dst       io.Writer
	remaining int64
	input     *extractionInput
}

func (w *extractionOutput) Write(p []byte) (int, error) {
	if w.input != nil && w.input.readErr != nil {
		return 0, w.input.readErr
	}
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(p)) > w.remaining {
		return 0, ErrArchive
	}
	n, err := w.dst.Write(p)
	w.remaining -= int64(n)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if cancelled := w.ctx.Err(); cancelled != nil {
		err = errors.Join(err, cancelled)
	}
	return n, err
}
