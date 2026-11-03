package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	hexencoding "encoding/hex"
	"errors"
	"fmt"
	"io"
)

var ErrArchiveWrite = errors.New("backup archive write failed")

// WriteArchive writes a format-v1 ZIP, including finalization. It does not sync,
// close, publish, or roll back dst; the caller must discard output on any error.
// open receives validated container names, not filesystem paths. Readers it
// returns are closed here. Cancellation is cooperative between I/O operations;
// it cannot interrupt a blocked open/Read/Write/Close call.
func WriteArchive(ctx context.Context, dst io.Writer, manifest []byte, open func(string) (io.ReadCloser, error)) error {
	if err := writeArchive(ctx, dst, manifest, open); err != nil {
		return fmt.Errorf("%w: %w", ErrArchiveWrite, err)
	}
	return nil
}

func writeArchive(ctx context.Context, dst io.Writer, manifest []byte, open func(string) (io.ReadCloser, error)) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if int64(len(manifest)) > MaxManifestBytes {
		return ErrManifest
	}
	// The bytes written must be the same bytes validated, even if the caller
	// reuses its input buffer after validation. Only bounded metadata is copied.
	data := bytes.Clone(manifest)
	m, err := DecodeManifest(bytes.NewReader(data))
	if err != nil {
		return err
	}
	output := &archiveOutput{ctx: ctx, dst: dst, remaining: MaxArchiveBytes}
	z := zip.NewWriter(output)
	defer func() { err = errors.Join(err, z.Close(), ctx.Err()) }()
	w, err := createEntry(z, "manifest.json")
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	if err := writeSource(ctx, z, m.Database, open); err != nil {
		return err
	}
	for _, im := range m.Images {
		if err := writeSource(ctx, z, File{Path: im.Path, Size: im.Size, SHA256: im.SHA256}, open); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func createEntry(z *zip.Writer, name string) (io.Writer, error) {
	h := &zip.FileHeader{Name: name, Method: zip.Store}
	h.SetMode(0600)
	return z.CreateHeader(h)
}

func writeSource(ctx context.Context, z *zip.Writer, file File, open func(string) (io.ReadCloser, error)) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := open(file.Path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, r.Close()) }()
	w, err := createEntry(z, file.Path)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.CopyBuffer(io.MultiWriter(w, h), io.LimitReader(contextInput{ctx: ctx, src: r}, file.Size+1), make([]byte, 32<<10))
	if err != nil {
		return err
	}
	if n != file.Size || hexencoding.EncodeToString(h.Sum(nil)) != file.SHA256 {
		return errors.New("backup source does not match manifest")
	}
	return ctx.Err()
}

type contextInput struct {
	ctx context.Context
	src io.Reader
}

func (r contextInput) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.src.Read(p)
}

type archiveOutput struct {
	ctx       context.Context
	dst       io.Writer
	remaining int64
}

func (w *archiveOutput) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(p)) > w.remaining {
		return 0, errors.New("backup ZIP size limit exceeded")
	}
	n, err := w.dst.Write(p)
	w.remaining -= int64(n)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	return n, err
}
