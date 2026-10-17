package platform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/windows"
)

var ErrUnsafeCoverDirectory = errors.New("unsupported or unsafe cover directory")

type coverDirectoryGuard struct {
	handles     []windows.Handle
	closeHandle func(windows.Handle) error
	once        sync.Once
	err         error
}

func (g *coverDirectoryGuard) Close() error {
	g.once.Do(func() {
		for i := len(g.handles) - 1; i >= 0; i-- {
			g.err = errors.Join(g.err, g.closeHandle(g.handles[i]))
		}
	})
	return g.err
}

// PrepareCoverDirectory holds the existing directory's ancestors and its fixed
// covers child until Close. It creates only that child, never removes anything,
// and does not authorize image file operations or replace application locking.
func PrepareCoverDirectory(ctx context.Context, directory string) (result io.Closer, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	paths, err := coverDirectoryPrefixes(directory)
	if err != nil {
		return nil, err
	}
	root, err := windows.UTF16PtrFromString(paths[0])
	if err != nil {
		return nil, err
	}
	if kind := windows.GetDriveType(root); kind != windows.DRIVE_FIXED && kind != windows.DRIVE_REMOVABLE && kind != windows.DRIVE_RAMDISK {
		return nil, ErrUnsafeCoverDirectory
	}
	guard := &coverDirectoryGuard{closeHandle: windows.CloseHandle}
	defer func() {
		if err != nil {
			err = errors.Join(err, guard.Close())
		}
	}()
	paths = append(paths, filepath.Join(directory, "covers"))
	for i, name := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p, err := windows.UTF16PtrFromString(name)
		if err != nil {
			return nil, err
		}
		if i == len(paths)-1 {
			// All existing parents are already checked and retained. Do not use
			// MkdirAll, follow an existing link, or remove a failed creation.
			if err := windows.CreateDirectory(p, nil); err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
				return nil, fmt.Errorf("create cover directory: %w", err)
			}
		}
		h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING,
			windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if err != nil {
			return nil, fmt.Errorf("hold cover directory: %w", err)
		}
		guard.handles = append(guard.handles, h)
		var info windows.ByHandleFileInformation
		if err := windows.GetFileInformationByHandle(h, &info); err != nil {
			return nil, err
		}
		kind, err := windows.GetFileType(h)
		if err != nil {
			return nil, err
		}
		if kind != windows.FILE_TYPE_DISK || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return nil, ErrUnsafeCoverDirectory
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return guard, nil
}

// Accept only canonical drive-letter paths; reject aliases rather than silently
// resolving a different namespace. Drive remapping/hostile attribute changes
// are outside this boundary's guarantee.
func coverDirectoryPrefixes(name string) ([]string, error) {
	volume := filepath.VolumeName(name)
	if len(volume) != 2 || volume[1] != ':' || !((volume[0] >= 'A' && volume[0] <= 'Z') || (volume[0] >= 'a' && volume[0] <= 'z')) || !filepath.IsAbs(name) || strings.ContainsAny(name, "/\x00") || filepath.Clean(name) != name {
		return nil, ErrUnsafeCoverDirectory
	}
	paths := []string{volume + `\`}
	if name == paths[0] {
		return nil, ErrUnsafeCoverDirectory
	}
	for _, component := range strings.Split(name[3:], `\`) {
		if !filepath.IsLocal(component) || strings.ContainsAny(component, `:<>"|?*`) || strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") {
			return nil, ErrUnsafeCoverDirectory
		}
		paths = append(paths, filepath.Join(paths[len(paths)-1], component))
	}
	return paths, nil
}
