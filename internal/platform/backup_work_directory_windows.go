package platform

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var ErrBackupWorkDirectoryUnsupported = errors.New("backup work directory capability unavailable")
var ErrBackupWorkDirectoryIdentity = errors.New("backup work directory identity or name mismatch")
var ErrBackupWorkDirectoryRelease = errors.New("backup work directory release failed")

type BackupWorkDirectoryIdentity struct {
	VolumeSerial uint64
	FileID       [16]byte
}

// Created records creation success, not verified ownership or cleanup permission.
// False does not prove absence after a native call failed.
type BackupWorkDirectoryError struct {
	Path    string
	Created bool
	Release BackupWorkDirectoryReleaseResult
	Err     error
}

func (e *BackupWorkDirectoryError) Error() string {
	return fmt.Sprintf("prepare backup work directory %q: %v", e.Path, e.Err)
}
func (e *BackupWorkDirectoryError) Unwrap() error { return e.Err }

// These observations cover the directory handle and its parents, not DB handles
// or permission to remove artifacts or resume application operations.
type BackupWorkDirectoryReleaseResult struct {
	HandleAcquired bool
	HandleClosed   bool
	CloseAttempted bool
	Parents        BackupDirectoryReleaseResult
}

func (r BackupWorkDirectoryReleaseResult) Confirmed() bool {
	return r.CloseAttempted && (!r.HandleAcquired || r.HandleClosed) && r.Parents.Confirmed()
}

// BackupWorkDirectory retains the newly created directory and all parents.
// Paths and IDs are observations, never permission to reopen or remove a path.
// Do not copy. Close only releases; it never deletes any work artifacts.
type BackupWorkDirectory struct {
	file             *os.File
	parent           io.Closer
	parentAttempted  bool
	path             string
	identity         BackupWorkDirectoryIdentity
	closeFile        func() error
	mu               sync.Mutex
	closed           bool
	err              error
	release          BackupWorkDirectoryReleaseResult
	discardAttempted bool
	discardResult    BackupWorkDirectoryDiscardResult
	discardErr       error
}

func (d *BackupWorkDirectory) Path() string                          { return d.path }
func (d *BackupWorkDirectory) Identity() BackupWorkDirectoryIdentity { return d.identity }

func (d *BackupWorkDirectory) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.closeLocked(nil)
}

// ReleaseResult waits for any in-progress Close/DiscardEmpty. A value snapshot
// separates release observations from deletion and absence-check errors.
func (d *BackupWorkDirectory) ReleaseResult() BackupWorkDirectoryReleaseResult {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.releaseResultLocked()
}

func (d *BackupWorkDirectory) releaseResultLocked() BackupWorkDirectoryReleaseResult {
	r := d.release
	r.HandleAcquired = d.file != nil || d.closeFile != nil
	return r
}

// Caller holds mu. Absence is checked only after successful child Close and
// before parent release. All release/check observations are cached once.
func (d *BackupWorkDirectory) closeLocked(absent func() error) error {
	if !d.closed {
		d.closed = true
		var releaseErr error
		if d.closeFile != nil {
			releaseErr = d.closeFile()
			d.release.HandleClosed = releaseErr == nil
		}
		if releaseErr == nil && absent != nil {
			d.err = absent()
		}
		if d.parent != nil {
			releaseErr = errors.Join(releaseErr, d.parent.Close())
			if p, ok := d.parent.(interface {
				ReleaseResult() BackupDirectoryReleaseResult
			}); ok {
				d.release.Parents = p.ReleaseResult()
			}
		} else if !d.parentAttempted {
			// No parent acquisition was attempted, rather than an unknown result.
			d.release.Parents = BackupDirectoryReleaseResult{CloseAttempted: true}
		}
		if releaseErr != nil {
			d.err = errors.Join(d.err, ErrBackupWorkDirectoryRelease, releaseErr)
		}
		d.release.CloseAttempted = true
	}
	return d.err
}

func (d *BackupWorkDirectory) VerifyIdentity() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return os.ErrClosed
	}
	id, err := identifyBackupWorkDirectory(d.file)
	if err != nil {
		return err
	}
	name, err := backupHandleName(d.file)
	if err != nil {
		return err
	}
	if id != d.identity || !strings.EqualFold(name, d.path) {
		return ErrBackupWorkDirectoryIdentity
	}
	return nil
}

var createBackupDirectoryProc = windows.NewLazySystemDLL("kernel32.dll").NewProc("CreateDirectory2W")

// CreateBackupWorkDirectory requires an existing app-data directory. No fallback,
// reuse, deletion or application admission is provided by this boundary.
func CreateBackupWorkDirectory(ctx context.Context, directory string) (*BackupWorkDirectory, error) {
	var token [16]byte
	if err := ctx.Err(); err != nil {
		return nil, backupWorkDirectoryUnacquiredError(err)
	}
	if _, err := rand.Read(token[:]); err != nil {
		return nil, backupWorkDirectoryUnacquiredError(err)
	}
	return createBackupWorkDirectory(ctx, directory, token, backupWorkDirectoryOps{
		find: createBackupDirectoryProc.Find, create: createBackupDirectoryHandle, identify: identifyBackupWorkDirectory, name: backupHandleName,
	})
}

type backupWorkDirectoryOps struct {
	find     func() error
	create   func(string) (*os.File, error)
	identify func(*os.File) (BackupWorkDirectoryIdentity, error)
	name     func(*os.File) (string, error)
	hold     func(context.Context, string) (io.Closer, error)
}

func backupWorkDirectoryUnacquiredError(err error) error {
	d := &BackupWorkDirectory{}
	_ = d.Close()
	return &BackupWorkDirectoryError{Release: d.ReleaseResult(), Err: err}
}

func createBackupWorkDirectory(ctx context.Context, directory string, token [16]byte, ops backupWorkDirectoryOps) (result *BackupWorkDirectory, err error) {
	d := &BackupWorkDirectory{}
	created := false
	defer func() {
		if err != nil {
			cause := errors.Join(err, d.Close())
			err = &BackupWorkDirectoryError{Path: d.path, Created: created, Release: d.ReleaseResult(), Err: cause}
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Validate before Join can normalize dots, separators or special namespaces.
	if _, err := coverDirectoryPrefixes(directory); err != nil {
		return nil, err
	}
	if err := ops.find(); err != nil {
		return nil, errors.Join(ErrBackupWorkDirectoryUnsupported, err)
	}
	hold := ops.hold
	if hold == nil {
		hold = func(ctx context.Context, path string) (io.Closer, error) { return HoldBackupDirectory(ctx, path) }
	}
	d.parentAttempted = true
	parent, err := hold(ctx, filepath.Join(directory, "backup-work-probe"))
	if err != nil {
		var detail *BackupDirectoryHoldError
		if errors.As(err, &detail) {
			d.release.Parents = detail.Release
		}
		return nil, err
	}
	d.parent = parent
	d.path = filepath.Join(directory, ".manga-update-backup-db-"+hex.EncodeToString(token[:]))
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	d.file, err = ops.create(d.path)
	if d.file != nil {
		d.closeFile = d.file.Close
	}
	if err != nil {
		return nil, err
	}
	created = true
	d.identity, err = ops.identify(d.file)
	if err != nil {
		return nil, err
	}
	name, err := ops.name(d.file)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(name, d.path) {
		return nil, ErrBackupWorkDirectoryIdentity
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return d, nil
}

func createBackupDirectoryHandle(name string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	h, _, err := createBackupDirectoryProc.Call(uintptr(unsafe.Pointer(p)), uintptr(windows.FILE_LIST_DIRECTORY|windows.FILE_READ_ATTRIBUTES|windows.DELETE|windows.SYNCHRONIZE), uintptr(windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE), 0, 0)
	if h == 0 || h == uintptr(windows.InvalidHandle) {
		if errors.Is(err, windows.ERROR_SUCCESS) {
			err = windows.ERROR_INVALID_HANDLE
		}
		return nil, err
	}
	return os.NewFile(h, name), nil
}

func identifyBackupWorkDirectory(file *os.File) (BackupWorkDirectoryIdentity, error) {
	h := windows.Handle(file.Fd())
	if err := checkBackupDirectory(h); err != nil {
		return BackupWorkDirectoryIdentity{}, err
	}
	var id BackupWorkDirectoryIdentity
	err := windows.GetFileInformationByHandleEx(h, windows.FileIdInfo, (*byte)(unsafe.Pointer(&id)), uint32(unsafe.Sizeof(id)))
	return id, err
}
