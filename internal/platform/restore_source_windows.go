package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/asam-masa/manga-update-manager/internal/backup"
	"golang.org/x/sys/windows"
)

var ErrRestoreSource = errors.New("restore source acquisition failed")
var ErrUnsafeRestoreSource = errors.New("restore source identity or bounds not confirmed")
var ErrRestoreSourceRelease = errors.New("restore source release not confirmed")

// Observations cover this input and its parents, not restore validity/admission.
type RestoreSourceReleaseResult struct {
	FileAcquired   bool
	FileClosed     bool
	CloseAttempted bool
	Parents        BackupDirectoryReleaseResult
}

func (r RestoreSourceReleaseResult) Confirmed() bool {
	return r.CloseAttempted && (!r.FileAcquired || r.FileClosed) && r.Parents.Confirmed()
}

type RestoreSourceError struct {
	Path    string
	Release RestoreSourceReleaseResult
	Err     error
}

func (e *RestoreSourceError) Error() string {
	return fmt.Sprintf("open restore source %q: %v", e.Path, e.Err)
}
func (e *RestoreSourceError) Unwrap() error { return e.Err }

type restoreSourceParents interface {
	Close() error
	ReleaseResult() BackupDirectoryReleaseResult
}

// Do not copy. Owns one read-only existing file and its parents. Close never
// deletes/reopens the input. Paths/IDs are observations, not delete permission.
type RestoreSource struct {
	mu              sync.Mutex
	file            *os.File
	parents         restoreSourceParents
	parentAttempted bool
	path            string
	identity        BackupTempIdentity
	size            int64
	closeFile       func() error
	release         RestoreSourceReleaseResult
	closeErr        error
}

func (s *RestoreSource) Path() string                 { return s.path }
func (s *RestoreSource) Identity() BackupTempIdentity { return s.identity }

func (s *RestoreSource) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.release.CloseAttempted {
		s.release.CloseAttempted = true
		if s.closeFile != nil {
			s.closeErr = s.closeFile()
			s.release.FileClosed = s.closeErr == nil
		}
		if s.parents != nil {
			s.closeErr = errors.Join(s.closeErr, s.parents.Close())
			s.release.Parents = s.parents.ReleaseResult()
		} else if !s.parentAttempted {
			s.release.Parents = BackupDirectoryReleaseResult{CloseAttempted: true}
		}
		if s.closeErr != nil || !s.release.Confirmed() {
			s.closeErr = errors.Join(ErrRestoreSourceRelease, s.closeErr)
		}
	}
	return s.closeErr
}

func (s *RestoreSource) ReleaseResult() RestoreSourceReleaseResult {
	if s == nil {
		return RestoreSourceReleaseResult{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.release
}

type restoreSourceObservation struct {
	id   BackupTempIdentity
	name string
	size int64
}

type restoreSourceOps struct {
	hold    func(context.Context, string) (restoreSourceParents, error)
	open    func(string) (*os.File, error)
	observe func(*os.File) (restoreSourceObservation, error)
	close   func(*os.File) error
}

// OpenRestoreSource requires canonical local NTFS paths and a single-link
// regular file. No writes, creation, deletion, copy fallback or raw handle API.
func OpenRestoreSource(ctx context.Context, name string) (*RestoreSource, error) {
	return openRestoreSource(ctx, name, restoreSourceOps{
		hold: func(ctx context.Context, path string) (restoreSourceParents, error) {
			guard, err := HoldBackupDirectory(ctx, path)
			if err != nil {
				return nil, err
			}
			return guard, nil
		},
		open: openRestoreSourceFile, observe: observeRestoreSource, close: (*os.File).Close,
	})
}

func openRestoreSource(ctx context.Context, name string, ops restoreSourceOps) (result *RestoreSource, err error) {
	s := &RestoreSource{path: name}
	defer func() {
		if err != nil {
			cause := errors.Join(ErrRestoreSource, err, s.Close())
			err = &RestoreSourceError{Path: name, Release: s.ReleaseResult(), Err: cause}
		}
	}()
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if _, err = ParseBackupDestination(name); err != nil {
		return nil, errors.Join(ErrUnsafeRestoreSource, err)
	}
	s.parentAttempted = true
	s.parents, err = ops.hold(ctx, name)
	if err != nil {
		var detail *BackupDirectoryHoldError
		if s.parents == nil && errors.As(err, &detail) {
			s.release.Parents = detail.Release
		}
		return nil, err
	}
	if s.parents == nil {
		return nil, ErrUnsafeRestoreSource
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	s.file, err = ops.open(name)
	if s.file != nil {
		s.release.FileAcquired = true
		s.closeFile = func() error { return ops.close(s.file) }
	}
	if err != nil {
		return nil, err
	}
	if s.file == nil {
		return nil, ErrUnsafeRestoreSource
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	observed, err := ops.observe(s.file)
	if err != nil {
		return nil, err
	}
	if observed.size < 22 || observed.size > backup.MaxArchiveBytes || !strings.EqualFold(observed.name, name) {
		return nil, ErrUnsafeRestoreSource
	}
	s.identity, s.size = observed.id, observed.size
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return s, nil
}

func openRestoreSourceFile(name string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), name), nil
}

func observeRestoreSource(file *os.File) (restoreSourceObservation, error) {
	id, err := identifyBackupTemp(file) // Kind, attributes, link count and file ID; no DB size bound.
	if err != nil {
		return restoreSourceObservation{}, errors.Join(ErrUnsafeRestoreSource, err)
	}
	name, err := backupHandleName(file)
	if err != nil {
		return restoreSourceObservation{}, err
	}
	info, err := file.Stat()
	if err != nil {
		return restoreSourceObservation{}, err
	}
	return restoreSourceObservation{id: id, name: name, size: info.Size()}, nil
}
