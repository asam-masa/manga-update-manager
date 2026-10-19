package application

import (
	"context"
	"errors"
	"strings"

	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
	"github.com/asam-masa/manga-update-manager/internal/thumbnail"
)

var ErrCoverUnavailable = errors.New("current cover unavailable")

type CoverReadRepository interface {
	WorkByID(context.Context, int64) (manga.Work, error)
	ManagedCoverByID(context.Context, string) (storage.ManagedCover, error)
}

type CoverReader struct {
	repository CoverReadRepository
	operations *CoverOperations
	directory  string
	read       func(context.Context, string, string, platform.CoverFileInfo) ([]byte, error)
}

func NewCoverReader(repository CoverReadRepository, operations *CoverOperations, directory string) *CoverReader {
	return &CoverReader{repository: repository, operations: operations, directory: directory, read: platform.ReadVerifiedCover}
}

// Read retains admission and reference exclusion through verification and all
// resource release. Returned bytes are a snapshot, not a live file reference.
func (a *CoverReader) Read(ctx context.Context, workID int64) (data []byte, err error) {
	if workID <= 0 {
		return nil, ErrCoverOperationInvalid
	}
	op, err := a.operations.BeginRead()
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, op.End())
		if err != nil {
			data = nil
		}
	}()
	stage := func(fn func() error) error {
		ticket, err := op.BeginReadStage()
		if err != nil {
			return err
		}
		err = ctx.Err()
		if err == nil {
			err = fn()
		}
		a.operations.ObserveError(err)
		if errors.Is(err, platform.ErrUnsafeCoverDirectory) {
			a.operations.Stop()
		}
		ticket.End()
		return err
	}
	var unlock func()
	if err = stage(func() error { var e error; unlock, e = a.operations.LockReferences(ctx); return e }); err != nil {
		return nil, err
	}
	defer unlock()
	var work manga.Work
	if err = stage(func() error { var e error; work, e = a.repository.WorkByID(ctx, workID); return e }); err != nil {
		return nil, err
	}
	path := work.ThumbnailPath
	if len(path) != len("covers/")+32+len(".png") || !strings.HasPrefix(path, "covers/") || !strings.HasSuffix(path, ".png") {
		return nil, ErrCoverUnavailable
	}
	id := path[len("covers/") : len(path)-len(".png")]
	for _, b := range []byte(id) {
		if !(b >= '0' && b <= '9') && !(b >= 'a' && b <= 'f') {
			return nil, ErrCoverUnavailable
		}
	}
	var record storage.ManagedCover
	if err = stage(func() error { var e error; record, e = a.repository.ManagedCoverByID(ctx, id); return e }); err != nil {
		return nil, err
	}
	if record.ID != id || record.RelativePath != path || record.State != "ready" || record.Info == nil {
		return nil, ErrCoverUnavailable
	}
	info := record.Info
	if err = stage(func() error {
		var e error
		data, e = a.read(ctx, a.directory, id, platform.CoverFileInfo{VolumeSerial: info.VolumeSerial, FileID: info.FileID, ByteSize: info.ByteSize, SHA256: info.SHA256})
		return e
	}); err != nil {
		return nil, err
	}
	if err = stage(func() error { return thumbnail.ValidateCover(data) }); err != nil {
		return nil, err
	}
	return data, nil
}
