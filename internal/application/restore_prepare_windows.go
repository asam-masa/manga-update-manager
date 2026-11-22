package application

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/platform"
)

var ErrRestorePrepare = errors.New("restore preparation failed")

type RestorePrepareError struct {
	OriginalErr   error
	SourceErr     error
	SourceRelease platform.RestoreSourceReleaseResult
	Cleanup       *platform.RestorePreparationCleanupResult
	CleanupErr    error
}

func (e *RestorePrepareError) Unwrap() error {
	return errors.Join(ErrRestorePrepare, e.OriginalErr, e.SourceErr, e.CleanupErr)
}
func (e *RestorePrepareError) Error() string { return fmt.Sprint(e.Unwrap()) }

// Metadata.Path is a container name. Reader borrows the retained output only;
// finish all consumers before the owning PreparedRestore is cleaned up.
type PreparedRestoreFile struct {
	Metadata backup.File
	ImageID  string // Empty for the database.
	Reader   io.ReaderAt
}

// Do not copy. Owns preparation files/parents, never the source ZIP. Success
// establishes byte preparation only, not DB/PNG validity or switch permission.
type PreparedRestore struct {
	preparation *platform.RestorePreparation
	manifest    backup.Manifest
	files       []PreparedRestoreFile
}

func (p *PreparedRestore) Manifest() backup.Manifest {
	m := p.manifest
	m.Migrations = slices.Clone(m.Migrations)
	m.Images = slices.Clone(m.Images)
	m.OmittedImages = slices.Clone(m.OmittedImages)
	return m
}
func (p *PreparedRestore) Files() []PreparedRestoreFile { return slices.Clone(p.files) }
func (p *PreparedRestore) Cleanup(originalErr error) (platform.RestorePreparationCleanupResult, error) {
	return p.preparation.Cleanup(originalErr)
}

type restorePrepareOps struct {
	open     func(context.Context, string) (*platform.RestoreSource, error)
	create   func(context.Context, string) (*platform.RestorePreparation, error)
	verify   func(context.Context, *platform.RestoreSource) error
	close    func(*platform.RestoreSource) (platform.RestoreSourceReleaseResult, error)
	validate func(io.ReaderAt, int64) (backup.Manifest, error)
	extract  func(context.Context, io.ReaderAt, int64, backup.Manifest, func(backup.File) (io.Writer, error)) error
	readback func(context.Context, *platform.RestorePreparationFile, backup.File) error
	cleanup  func(*platform.RestorePreparation, error) (platform.RestorePreparationCleanupResult, error)
}

func restorePreparationOperations() restorePrepareOps {
	return restorePrepareOps{
		open: platform.OpenRestoreSource, create: platform.CreateRestorePreparation,
		verify: func(ctx context.Context, s *platform.RestoreSource) error { return s.VerifyIdentity(ctx) },
		close: func(s *platform.RestoreSource) (platform.RestoreSourceReleaseResult, error) {
			err := s.Close()
			return s.ReleaseResult(), err
		},
		validate: backup.ValidateArchive, extract: backup.ExtractArchive,
		readback: func(ctx context.Context, f *platform.RestorePreparationFile, expected backup.File) error {
			digest, err := hex.DecodeString(expected.SHA256)
			if err != nil || len(digest) != 32 {
				return backup.ErrManifest
			}
			return VerifyRestorePreparationFile(ctx, f, expected.Size, [32]byte(digest))
		},
		cleanup: (*platform.RestorePreparation).Cleanup,
	}
}

// PrepareRestore retains the source, validates the whole ZIP before creating
// output, revalidates while extracting, and syncs/reads back every output. Only
// after final source checks AND confirmed source release is output ownership
// returned. The caller owns admission and supplies its validated data directory;
// this internal operation does not stop/restart normal application operations.
// On failure no usable output is returned; owned cleanup finishes even after
// cancellation. No source/current/retired data is modified or deleted.
func PrepareRestore(ctx context.Context, sourcePath, directory string) (*PreparedRestore, error) {
	return prepareRestore(ctx, sourcePath, directory, restorePreparationOperations())
}

func prepareRestore(ctx context.Context, sourcePath, directory string, ops restorePrepareOps) (result *PreparedRestore, err error) {
	var source *platform.RestoreSource
	var preparation *platform.RestorePreparation
	defer func() {
		detail := &RestorePrepareError{OriginalErr: errors.Join(err, ctx.Err())}
		if source != nil {
			detail.SourceRelease, detail.SourceErr = ops.close(source)
			if !detail.SourceRelease.Confirmed() {
				detail.SourceErr = errors.Join(platform.ErrRestoreSourceRelease, detail.SourceErr)
			}
		} else {
			var acquired *platform.RestoreSourceError
			if errors.As(err, &acquired) {
				detail.SourceRelease = acquired.Release
			}
		}
		detail.OriginalErr = errors.Join(detail.OriginalErr, ctx.Err())
		if detail.OriginalErr == nil && detail.SourceErr == nil {
			return
		}
		result = nil
		if preparation != nil {
			cleanup, cleanupErr := ops.cleanup(preparation, errors.Join(detail.OriginalErr, detail.SourceErr))
			detail.Cleanup, detail.CleanupErr = &cleanup, cleanupErr
			if !cleanup.ParentRelease.Confirmed() {
				detail.CleanupErr = errors.Join(platform.ErrRestorePreparationCleanup, detail.CleanupErr)
			}
			for _, c := range cleanup.Children {
				if !c.Release.Confirmed() {
					detail.CleanupErr = errors.Join(platform.ErrRestorePreparationCleanup, detail.CleanupErr)
				}
			}
		}
		detail.OriginalErr = errors.Join(detail.OriginalErr, ctx.Err())
		err = detail
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	source, err = ops.open(ctx, sourcePath)
	if err != nil {
		return nil, err
	}
	if source == nil {
		return nil, platform.ErrRestoreSource
	}
	size, err := source.Size(ctx)
	if err != nil {
		return nil, err
	}
	if err := ops.verify(ctx, source); err != nil {
		return nil, err
	}
	m, err := ops.validate(source.Reader(ctx), size)
	if err != nil {
		return nil, err
	}
	if err := ops.verify(ctx, source); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	preparation, err = ops.create(ctx, directory)
	if err != nil {
		return nil, err
	}
	if preparation == nil {
		return nil, platform.ErrRestorePreparationCleanup
	}
	targets := map[string]PreparedRestoreFile{m.Database.Path: {Metadata: m.Database}}
	for _, im := range m.Images {
		targets[im.Path] = PreparedRestoreFile{Metadata: backup.File{Path: im.Path, Size: im.Size, SHA256: im.SHA256}, ImageID: im.ID}
	}
	var files []PreparedRestoreFile
	var outputs []*platform.RestorePreparationFile
	err = ops.extract(ctx, source.Reader(ctx), size, m, func(meta backup.File) (io.Writer, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		target, ok := targets[meta.Path]
		if !ok || meta != target.Metadata {
			return nil, backup.ErrManifest
		}
		file, err := preparation.CreateFile(ctx)
		if err != nil {
			return nil, err
		}
		delete(targets, meta.Path)
		target.Reader = restorePreparedReader{file}
		files, outputs = append(files, target), append(outputs, file)
		return file, nil
	})
	if err != nil {
		return nil, err
	}
	if len(targets) != 0 {
		return nil, backup.ErrArchiveExtract
	}
	for i, file := range outputs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := ops.readback(ctx, file, files[i].Metadata); err != nil {
			return nil, err
		}
	}
	if err := ops.verify(ctx, source); err != nil {
		return nil, err
	}
	return &PreparedRestore{preparation: preparation, manifest: m, files: files}, nil
}

// Expose only ReadAt when lending the output; not its Close/Write/Sync methods.
type restorePreparedReader struct {
	file *platform.RestorePreparationFile
}

func (r restorePreparedReader) ReadAt(p []byte, off int64) (int, error) { return r.file.ReadAt(p, off) }
