package platform

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/windows"
)

// ErrStorageInUse means another handle prevents exclusive use of the lock file.
var ErrStorageInUse = errors.New("storage is already in use")

const storageLockName = ".instance.lock"

type storageLock struct {
	handle windows.Handle
	once   sync.Once
	err    error
}

func (l *storageLock) Close() error {
	l.once.Do(func() { l.err = windows.CloseHandle(l.handle) })
	return l.err
}

// AcquireStorageLock holds an OS handle, not a file-existence or PID lock.
// The caller keeps it until all DB/file operations have stopped. Never remove
// the persistent lock file: removal could let callers lock different files.
func AcquireStorageLock(directory string) (io.Closer, error) {
	if !filepath.IsAbs(directory) {
		return nil, errors.New("storage directory must be absolute")
	}
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() {
		return nil, errors.New("storage directory is unavailable")
	}
	name, err := windows.UTF16PtrFromString(filepath.Join(directory, storageLockName))
	if err != nil {
		return nil, fmt.Errorf("encode lock name: %w", err)
	}
	// OPEN_ALWAYS preserves existing contents. No sharing also blocks rename
	// and deletion while held. A nil security descriptor prevents inheritance.
	handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE,
		0, nil, windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		if errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			return nil, ErrStorageInUse
		}
		return nil, fmt.Errorf("open storage lock: %w", err)
	}
	lock := &storageLock{handle: handle}
	var fileInfo windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(handle, &fileInfo); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("inspect storage lock: %w", err)
	}
	fileType, err := windows.GetFileType(handle)
	if err != nil || fileType != windows.FILE_TYPE_DISK ||
		fileInfo.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 ||
		fileInfo.NumberOfLinks != 1 {
		_ = lock.Close()
		return nil, errors.New("storage lock must be an ordinary unlinked file")
	}
	return lock, nil
}
