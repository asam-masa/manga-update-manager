package application

import (
	"context"
	"io"
	"io/fs"
	"sync"
	"time"
)

const restoreSQLName = "prepared.sqlite"

type restoreSQLFS struct {
	ctx    context.Context
	reader io.ReaderAt
	size   int64

	mu        sync.Mutex
	fault     error
	openViews int
}

func (b *restoreSQLFS) Open(name string) (fs.File, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if name != restoreSQLName {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	if err := b.readiness(); err != nil {
		return nil, err
	}
	b.openViews++
	return &restoreSQLView{owner: b, section: io.NewSectionReader(b, 0, b.size)}, nil
}

// readiness is called with mu held.
func (b *restoreSQLFS) readiness() error {
	if b.fault != nil {
		return b.fault
	}
	return b.ctx.Err()
}

func (b *restoreSQLFS) ReadAt(p []byte, offset int64) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.readiness(); err != nil {
		return 0, err
	}
	n, err := b.reader.ReadAt(p, offset)
	if err == nil && n != len(p) || err == io.EOF && int64(n) < b.size-offset && n < len(p) {
		err = io.ErrUnexpectedEOF
	}
	if err != nil && err != io.EOF {
		b.fault = err
	}
	return n, err
}

type restoreSQLView struct {
	owner   *restoreSQLFS
	section *io.SectionReader
	mu      sync.Mutex
	closed  bool
}

func (v *restoreSQLView) check() error {
	if v.closed {
		return fs.ErrClosed
	}
	v.owner.mu.Lock()
	defer v.owner.mu.Unlock()
	return v.owner.readiness()
}

func (v *restoreSQLView) Read(p []byte) (int, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.check(); err != nil {
		return 0, err
	}
	return v.section.Read(p)
}

func (v *restoreSQLView) Seek(offset int64, whence int) (int64, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.check(); err != nil {
		return 0, err
	}
	return v.section.Seek(offset, whence)
}

func (v *restoreSQLView) Stat() (fs.FileInfo, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.check(); err != nil {
		return nil, err
	}
	return restoreSQLInfo{size: v.owner.size}, nil
}

func (v *restoreSQLView) Close() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.closed {
		v.closed = true
		v.owner.mu.Lock()
		v.owner.openViews--
		v.owner.mu.Unlock()
	}
	return nil
}

type restoreSQLInfo struct{ size int64 }

func (i restoreSQLInfo) Name() string     { return restoreSQLName }
func (i restoreSQLInfo) Size() int64      { return i.size }
func (restoreSQLInfo) Mode() fs.FileMode  { return 0444 }
func (restoreSQLInfo) ModTime() time.Time { return time.Time{} }
func (restoreSQLInfo) IsDir() bool        { return false }
func (restoreSQLInfo) Sys() any           { return nil }
