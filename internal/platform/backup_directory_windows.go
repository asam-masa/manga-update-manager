package platform

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/windows"
)

var ErrUnsafeBackupDirectory = errors.New("unsupported or unsafe backup directory")
var ErrBackupDirectoryRelease = errors.New("backup directory release failed")

// BackupDirectoryReleaseResult describes only this owner's parent holds.
// It is not permission to resume application operations or remove any path.
type BackupDirectoryReleaseResult struct {
	Acquired       int
	Released       int
	CloseAttempted bool
}

func (r BackupDirectoryReleaseResult) Confirmed() bool {
	return r.CloseAttempted && r.Acquired >= 0 && r.Released == r.Acquired
}

// BackupDirectoryHoldError also reports release after partial acquisition.
type BackupDirectoryHoldError struct {
	Release BackupDirectoryReleaseResult
	Err     error
}

func (e *BackupDirectoryHoldError) Error() string { return e.Err.Error() }
func (e *BackupDirectoryHoldError) Unwrap() error { return e.Err }

// BackupDirectoryGuard owns the retained parents. Do not copy.
type BackupDirectoryGuard struct {
	handles  []windows.Handle
	close    func(windows.Handle) error
	mu       sync.Mutex
	closed   bool
	released int
	err      error
}

func (g *BackupDirectoryGuard) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.closed {
		g.closed = true
		for i := len(g.handles) - 1; i >= 0; i-- {
			if err := g.close(g.handles[i]); err != nil {
				g.err = errors.Join(g.err, ErrBackupDirectoryRelease, err)
			} else {
				g.released++
			}
		}
	}
	return g.err
}

// ReleaseResult waits for any in-progress Close and returns a value snapshot.
func (g *BackupDirectoryGuard) ReleaseResult() BackupDirectoryReleaseResult {
	g.mu.Lock()
	defer g.mu.Unlock()
	return BackupDirectoryReleaseResult{Acquired: len(g.handles), Released: g.released, CloseAttempted: g.closed}
}

// HoldBackupDirectory validates the destination's existing local NTFS parents
// and retains them without delete sharing until Close. It does not open/create
// the destination file, authorize deletion, or prove write/publish capability.
// Drive remapping and hostile attribute changes are outside this guarantee.
func HoldBackupDirectory(ctx context.Context, destination string) (*BackupDirectoryGuard, error) {
	return holdBackupDirectory(ctx, destination, backupDirectoryOps{
		drive: func(root string) uint32 {
			p, _ := windows.UTF16PtrFromString(root) // Already lexically checked.
			return windows.GetDriveType(p)
		},
		open: func(name string) (windows.Handle, error) {
			p, err := windows.UTF16PtrFromString(name)
			if err != nil {
				return 0, err
			}
			return windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
				nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		},
		check: checkBackupDirectory,
		close: windows.CloseHandle,
	})
}

// This private seam tests partial acquisition and release failures without
// changing real volumes, permissions or handle values in the operating system.
type backupDirectoryOps struct {
	drive func(string) uint32
	open  func(string) (windows.Handle, error)
	check func(windows.Handle) error
	close func(windows.Handle) error
}

func holdBackupDirectory(ctx context.Context, destination string, ops backupDirectoryOps) (result *BackupDirectoryGuard, err error) {
	guard := &BackupDirectoryGuard{close: ops.close}
	defer func() {
		if err != nil {
			cause := errors.Join(err, guard.Close())
			err = &BackupDirectoryHoldError{Release: guard.ReleaseResult(), Err: cause}
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	parsed, err := ParseBackupDestination(destination)
	if err != nil {
		return nil, err
	}
	root := filepath.VolumeName(parsed.Directory) + `\`
	if kind := ops.drive(root); kind != windows.DRIVE_FIXED && kind != windows.DRIVE_REMOVABLE {
		return nil, ErrUnsafeBackupDirectory
	}
	paths := []string{root}
	if parsed.Directory != root {
		for _, part := range strings.Split(parsed.Directory[3:], `\`) {
			paths = append(paths, filepath.Join(paths[len(paths)-1], part))
		}
	}
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		h, e := ops.open(path)
		if e != nil {
			return nil, fmt.Errorf("hold backup directory: %w", e)
		}
		guard.handles = append(guard.handles, h)
		if e := ops.check(h); e != nil {
			return nil, fmt.Errorf("check backup directory: %w", e)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return guard, nil
}

func checkBackupDirectory(h windows.Handle) error {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return err
	}
	kind, err := windows.GetFileType(h)
	if err != nil {
		return err
	}
	if kind != windows.FILE_TYPE_DISK || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 ||
		info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return ErrUnsafeBackupDirectory
	}
	var fs [261]uint16
	if err := windows.GetVolumeInformationByHandle(h, nil, 0, nil, nil, nil, &fs[0], uint32(len(fs))); err != nil {
		return err
	}
	return checkBackupFileSystem(windows.UTF16ToString(fs[:]))
}

func checkBackupFileSystem(name string) error {
	if name != "NTFS" {
		return ErrUnsafeBackupDirectory
	}
	return nil
}
