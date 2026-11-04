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

var ErrBackupTempRelease = errors.New("backup temporary file release failed")

// BackupTempIdentity identifies an opened file, not a path or deletion permit.
type BackupTempIdentity struct {
	VolumeSerial uint64
	FileID       [16]byte
}

// BackupTempError reports whether CREATE_NEW succeeded. Created does not mean
// identity was verified, or that Path can safely be reopened or deleted.
type BackupTempError struct {
	Path    string
	Created bool
	Err     error
}

func (e *BackupTempError) Error() string {
	return fmt.Sprintf("prepare backup temporary %q: %v", e.Path, e.Err)
}
func (e *BackupTempError) Unwrap() error { return e.Err }

// BackupTemp retains one newly created file and its parents. Close releases them;
// it neither deletes nor publishes the file. The raw handle stays private.
type BackupTemp struct {
	file      *os.File
	directory io.Closer
	path      string
	identity  BackupTempIdentity
	closeFile func() error
	mu        sync.Mutex
	closed    bool
	once      sync.Once
	err       error
}

func (t *BackupTemp) Path() string                 { return t.path }
func (t *BackupTemp) Identity() BackupTempIdentity { return t.identity }

func (t *BackupTemp) Close() error {
	t.once.Do(func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.closed = true
		if t.closeFile != nil {
			if err := t.closeFile(); err != nil {
				t.err = errors.Join(t.err, ErrBackupTempRelease, err)
			}
		}
		if t.directory != nil {
			t.err = errors.Join(t.err, t.directory.Close())
		}
	})
	return t.err
}

func (t *BackupTemp) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.file == nil {
		return 0, os.ErrClosed
	}
	return t.file.Write(p)
}

func (t *BackupTemp) ReadAt(p []byte, offset int64) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.file == nil {
		return 0, os.ErrClosed
	}
	return t.file.ReadAt(p, offset)
}

func (t *BackupTemp) Sync() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.file == nil {
		return os.ErrClosed
	}
	return t.file.Sync()
}

func (t *BackupTemp) Size() (int64, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.file == nil {
		return 0, os.ErrClosed
	}
	info, err := t.file.Stat()
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// VerifyIdentity rechecks the held file, not the file currently named by Path.
func (t *BackupTemp) VerifyIdentity() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.file == nil {
		return os.ErrClosed
	}
	actual, err := identifyBackupTemp(t.file)
	if err != nil {
		return err
	}
	if actual != t.identity {
		return errors.New("backup temporary identity changed")
	}
	return nil
}

// CreateBackupTemp creates a dedicated random name next to destination without
// overwriting anything. Failure after creation preserves the incomplete file
// and returns its location. Publication and owned cleanup come later.
func CreateBackupTemp(ctx context.Context, destination string) (*BackupTemp, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, err
	}
	return createBackupTemp(ctx, destination, token, identifyBackupTemp)
}

func createBackupTemp(ctx context.Context, destination string, token [16]byte, identify func(*os.File) (BackupTempIdentity, error)) (result *BackupTemp, err error) {
	directory, err := HoldBackupDirectory(ctx, destination)
	if err != nil {
		return nil, err
	}
	temp := &BackupTemp{directory: directory}
	created := false
	defer func() {
		if err != nil {
			err = &BackupTempError{Path: temp.path, Created: created, Err: errors.Join(err, temp.Close())}
		}
	}()
	parsed, err := ParseBackupDestination(destination)
	if err != nil {
		return nil, err
	}
	temp.path = filepath.Join(parsed.Directory, ".manga-update-backup-"+hex.EncodeToString(token[:])+".partial")
	if strings.EqualFold(temp.path, destination) {
		return nil, ErrInvalidBackupDestination
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p, err := windows.UTF16PtrFromString(temp.path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE|windows.DELETE, 0, nil,
		windows.CREATE_NEW, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	created = true
	temp.file = os.NewFile(uintptr(h), temp.path)
	temp.closeFile = temp.file.Close
	temp.identity, err = identify(temp.file)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return temp, nil
}

func identifyBackupTemp(file *os.File) (BackupTempIdentity, error) {
	h := windows.Handle(file.Fd())
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return BackupTempIdentity{}, err
	}
	kind, err := windows.GetFileType(h)
	if err != nil {
		return BackupTempIdentity{}, err
	}
	if kind != windows.FILE_TYPE_DISK || info.NumberOfLinks != 1 ||
		info.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0 {
		return BackupTempIdentity{}, ErrUnsafeBackupDirectory
	}
	var identity BackupTempIdentity
	if err := windows.GetFileInformationByHandleEx(h, windows.FileIdInfo, (*byte)(unsafe.Pointer(&identity)), uint32(unsafe.Sizeof(identity))); err != nil {
		return BackupTempIdentity{}, err
	}
	return identity, nil
}
