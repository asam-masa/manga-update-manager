//go:build windows

// Package workdirectory probes public APIs using t.TempDir fixtures only.
// It is not imported by the product and grants no cleanup permission.
package workdirectory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"github.com/asam-masa/manga-update-manager/internal/platform"
	"golang.org/x/sys/windows"
)

var createDirectory2 = windows.NewLazySystemDLL("kernel32.dll").NewProc("CreateDirectory2W")

type identity struct {
	Volume uint64
	ID     [16]byte
}

func identify(t *testing.T, h windows.Handle) identity {
	t.Helper()
	var id identity
	if err := windows.GetFileInformationByHandleEx(h, windows.FileIdInfo, (*byte)(unsafe.Pointer(&id)), uint32(unsafe.Sizeof(id))); err != nil {
		t.Fatal(err)
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		t.Fatal(err)
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		t.Fatal("unsafe directory attributes")
	}
	return id
}

func create(name string) (windows.Handle, error) {
	// Existing parents are checked/held by HoldBackupDirectory in both callers.
	// DISALLOW_PATH_REDIRECTS is probed separately; it rejects input locally.
	return createWith(name, windows.FILE_LIST_DIRECTORY|windows.FILE_READ_ATTRIBUTES|windows.DELETE|windows.SYNCHRONIZE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, 0)
}

func createWith(name string, access, share, flags uint32) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	// Public signature: path, desired access, share, DIRECTORY_FLAGS, security.
	h, _, err := createDirectory2.Call(uintptr(unsafe.Pointer(p)), uintptr(access), uintptr(share), uintptr(flags), 0)
	if h == 0 || h == uintptr(windows.InvalidHandle) {
		return 0, err
	}
	return windows.Handle(h), nil
}

func dispose(h windows.Handle) error {
	var remove byte = 1 // FILE_DISPOSITION_INFO uses BOOLEAN, not BOOL.
	return windows.SetFileInformationByHandle(h, windows.FileDispositionInfo, &remove, 1)
}

func TestCreatedDirectoryHandle(t *testing.T) {
	if err := createDirectory2.Find(); err != nil {
		t.Skipf("CreateDirectory2W unavailable: %v", err)
	}
	root := t.TempDir()
	parent, err := platform.HoldBackupDirectory(context.Background(), filepath.Join(root, "probe.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := parent.Close(); err != nil {
			t.Error(err)
		}
	}()
	name := filepath.Join(root, "work")
	h, err := create(name)
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		t.Skipf("documented creation inputs rejected; capability NOT verified: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	defer func() {
		if !closed {
			if err := windows.CloseHandle(h); err != nil {
				t.Error(err)
			}
		}
	}()
	id := identify(t, h)
	if duplicate, err := create(name); err == nil {
		windows.CloseHandle(duplicate)
		t.Fatal("existing directory accepted")
	} else if !errors.Is(err, windows.ERROR_ALREADY_EXISTS) && !errors.Is(err, windows.ERROR_FILE_EXISTS) {
		t.Fatalf("unexpected collision: %v", err)
	}
	if err := os.Rename(name, name+"-moved"); err == nil {
		t.Fatal("held directory renamed")
	}
	if err := os.WriteFile(filepath.Join(name, "synthetic.db"), []byte("not a real database"), 0600); err != nil {
		t.Fatal("child write", err)
	}
	if got := identify(t, h); got != id {
		t.Fatal("directory identity changed")
	}
	if err := dispose(h); !errors.Is(err, windows.ERROR_DIR_NOT_EMPTY) {
		t.Fatalf("nonempty disposal: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(name, "synthetic.db"))
	if err != nil || string(content) != "not a real database" {
		t.Fatal("nonempty contents changed", err)
	}
	if err := windows.CloseHandle(h); err != nil {
		t.Fatal(err)
	}
	closed = true
	if err := os.Rename(name, name+"-moved"); err != nil {
		t.Fatal("close did not release directory", err)
	}
	t.Log("created handle, collision refusal, identity, rename protection, child I/O, nonempty refusal: verified")
}

func TestEmptyDirectoryDisposition(t *testing.T) {
	if err := createDirectory2.Find(); err != nil {
		t.Skipf("CreateDirectory2W unavailable: %v", err)
	}
	root := t.TempDir()
	parent, err := platform.HoldBackupDirectory(context.Background(), filepath.Join(root, "probe.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := parent.Close(); err != nil {
			t.Error(err)
		}
	}()
	name := filepath.Join(root, "empty")
	h, err := create(name)
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		t.Skipf("documented creation inputs rejected; capability NOT verified: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	defer func() {
		if !closed {
			if err := windows.CloseHandle(h); err != nil {
				t.Error(err)
			}
		}
	}()
	id := identify(t, h)
	if err := dispose(h); err != nil {
		t.Fatal(err)
	}
	if got := identify(t, h); got != id {
		t.Fatal("disposition changed identity")
	}
	if err := windows.CloseHandle(h); err != nil {
		t.Fatal(err)
	}
	closed = true
	if _, err := os.Stat(name); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("empty directory not removed", err)
	}
	t.Log("same-handle empty disposition and absence after Close: verified")
}

func TestDocumentedMinimalCreation(t *testing.T) {
	if err := createDirectory2.Find(); err != nil {
		t.Skipf("CreateDirectory2W unavailable: %v", err)
	}
	name := filepath.Join(t.TempDir(), "minimal")
	h, err := createWith(name, windows.FILE_LIST_DIRECTORY|windows.SYNCHRONIZE, windows.FILE_SHARE_READ, 0)
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		t.Skipf("minimal documented inputs rejected; capability NOT verified: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := windows.CloseHandle(h); err != nil {
			t.Error(err)
		}
	}()
	t.Log("minimal creation only: no DELETE access, redirect flag, child I/O or cleanup guarantee")
}

func TestCreationInputMatrix(t *testing.T) {
	if err := createDirectory2.Find(); err != nil {
		t.Skipf("CreateDirectory2W unavailable: %v", err)
	}
	for _, tc := range []struct {
		name          string
		access, flags uint32
	}{
		{"attributes", windows.FILE_LIST_DIRECTORY | windows.FILE_READ_ATTRIBUTES | windows.SYNCHRONIZE, 0},
		{"attributes-redirect", windows.FILE_LIST_DIRECTORY | windows.FILE_READ_ATTRIBUTES | windows.SYNCHRONIZE, 1},
		{"delete", windows.FILE_LIST_DIRECTORY | windows.FILE_READ_ATTRIBUTES | windows.DELETE | windows.SYNCHRONIZE, 0},
		{"delete-redirect", windows.FILE_LIST_DIRECTORY | windows.FILE_READ_ATTRIBUTES | windows.DELETE | windows.SYNCHRONIZE, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, err := createWith(filepath.Join(t.TempDir(), "matrix"), tc.access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, tc.flags)
			if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
				t.Skipf("input combination rejected; capability NOT verified: %v", err)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := windows.CloseHandle(h); err != nil {
					t.Error(err)
				}
			}()
			_ = identify(t, h)
			t.Log("creation and identity verified for this input combination only")
		})
	}
}
