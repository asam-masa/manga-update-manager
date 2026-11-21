package platform

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/windows"
)

var ErrRestorePreparationFileIdentity = errors.New("restore preparation file identity not confirmed")
var ErrRestorePreparationFileRelease = errors.New("restore preparation file release not confirmed")

// Only the child handle is covered. This does not report parent release,
// deletion, absence, or permission to resume application operations.
type RestorePreparationFileRelease struct {
	CloseAttempted bool
	HandleAcquired bool
	HandleClosed   bool
}

func (r RestorePreparationFileRelease) Confirmed() bool {
	return r.CloseAttempted && (!r.HandleAcquired || r.HandleClosed)
}

// Created=false is not proof of absence after a failed native call. Neither
// creation nor Path grants deletion permission. Errors retain release evidence.
type RestorePreparationFileError struct {
	Path    string
	Created bool
	Release RestorePreparationFileRelease
	Err     error
}

func (e *RestorePreparationFileError) Error() string {
	return fmt.Sprintf("prepare restore file %q: %v", e.Path, e.Err)
}
func (e *RestorePreparationFileError) Unwrap() error { return e.Err }

// Do not copy. The caller keeps work and all its parents alive until children
// finish. Close never initiates deletion or releases work. No raw handle is exported.
type RestorePreparationFile struct {
	mu               sync.Mutex
	file             *os.File
	path             string
	identity         BackupTempIdentity
	closeFile        func() error
	release          RestorePreparationFileRelease
	closeErr         error
	work             *BackupWorkDirectory // Borrowed; caller retains it through child cleanup.
	discardAttempted bool
	discardResult    RestorePreparationDiscardResult
	discardErr       error
}

func (f *RestorePreparationFile) Path() string                 { return f.path }
func (f *RestorePreparationFile) Identity() BackupTempIdentity { return f.identity }
func (f *RestorePreparationFile) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closeLocked()
}
func (f *RestorePreparationFile) closeLocked() error {
	if !f.release.CloseAttempted {
		f.release.CloseAttempted = true
		if f.closeFile != nil {
			if err := f.closeFile(); err != nil {
				f.closeErr = errors.Join(ErrRestorePreparationFileRelease, err)
			} else {
				f.release.HandleClosed = true
			}
		}
	}
	return f.closeErr
}
func (f *RestorePreparationFile) ReleaseResult() RestorePreparationFileRelease {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.release
}

type restorePreparationFileOps struct {
	create  func(string) (*os.File, error)
	observe func(*os.File) (backupDBObservation, error)
	close   func(*os.File) error
}

// CreateRestorePreparationFile borrows an existing, retained work directory.
// It never reopens/releases parents, accepts archive paths, or overwrites files.
// Success transfers child ownership; creation failure attempts release only.
func CreateRestorePreparationFile(ctx context.Context, work *BackupWorkDirectory) (*RestorePreparationFile, error) {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, &RestorePreparationFileError{Release: RestorePreparationFileRelease{CloseAttempted: true}, Err: err}
	}
	return createRestorePreparationFile(ctx, work, token, restorePreparationFileOps{
		create: createRestorePreparationFileHandle, observe: observeBackupDB, close: func(f *os.File) error { return f.Close() },
	})
}

func createRestorePreparationFile(ctx context.Context, work *BackupWorkDirectory, token [16]byte, ops restorePreparationFileOps) (result *RestorePreparationFile, err error) {
	f := &RestorePreparationFile{}
	created := false
	locked := false
	defer func() {
		if err != nil {
			err = errors.Join(err, f.Close())
			err = &RestorePreparationFileError{Path: f.path, Created: created, Release: f.ReleaseResult(), Err: err}
		}
		if locked {
			work.mu.Unlock()
		}
	}()
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if work == nil {
		return nil, ErrRestorePreparationFileIdentity
	}
	// Serialize creation/verification against parent Close/DiscardEmpty. Child
	// lifetimes after return remain the caller's responsibility, not a lease.
	work.mu.Lock()
	locked = true
	if work.closed || work.file == nil {
		return nil, os.ErrClosed
	}
	id, err := identifyBackupWorkDirectory(work.file)
	if err != nil {
		return nil, err
	}
	name, err := backupHandleName(work.file)
	if err != nil {
		return nil, err
	}
	if id != work.identity || !strings.EqualFold(name, work.path) {
		return nil, ErrBackupWorkDirectoryIdentity
	}
	f.work = work
	f.path = filepath.Join(work.path, ".manga-update-restore-"+hex.EncodeToString(token[:])+".partial")
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	f.file, err = ops.create(f.path)
	if f.file != nil {
		f.release.HandleAcquired = true
		f.closeFile = func() error { return ops.close(f.file) }
	}
	if err != nil {
		return nil, err
	}
	if f.file == nil {
		return nil, ErrRestorePreparationFileIdentity
	}
	created = true
	observed, err := ops.observe(f.file)
	if err != nil {
		return nil, err
	}
	if observed.size != 0 || !strings.EqualFold(observed.name, f.path) {
		return nil, ErrRestorePreparationFileIdentity
	}
	f.identity = observed.id
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return f, nil
}

func createRestorePreparationFileHandle(name string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE|windows.DELETE, 0, nil, windows.CREATE_NEW, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), name), nil
}
