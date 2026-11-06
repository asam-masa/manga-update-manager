//go:build windows

// Package dbhandle probes SQLite/file-handle interoperability in t.TempDir only.
// It is not a product source acquisition or cleanup implementation.
package dbhandle

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"github.com/asam-masa/manga-update-manager/internal/platform"
	"golang.org/x/sys/windows"
	_ "modernc.org/sqlite"
)

type identity struct {
	Volume uint64
	ID     [16]byte
}

func id(t *testing.T, f *os.File) identity {
	t.Helper()
	var result identity
	if err := windows.GetFileInformationByHandleEx(windows.Handle(f.Fd()), windows.FileIdInfo, (*byte)(unsafe.Pointer(&result)), uint32(unsafe.Sizeof(result))); err != nil {
		t.Fatal(err)
	}
	return result
}

func root(t *testing.T) string {
	t.Helper()
	f, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	buf := make([]uint16, 32768)
	n, err := windows.GetFinalPathNameByHandle(windows.Handle(f.Fd()), &buf[0], uint32(len(buf)), 0)
	if err != nil || n == 0 || n >= uint32(len(buf)) {
		t.Fatal(n, err)
	}
	return strings.TrimPrefix(windows.UTF16ToString(buf[:n]), `\\?\`)
}

func create(name string, access, share uint32) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, access, share, nil, windows.CREATE_NEW, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), name), nil
}

func TestVacuumIntoRetainedFile(t *testing.T) {
	for _, tc := range []struct {
		name          string
		access, share uint32
		works         bool
	}{
		{"delete-shared", windows.GENERIC_READ | windows.GENERIC_WRITE | windows.DELETE, windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE, false},
		{"readwrite-shared", windows.GENERIC_READ | windows.GENERIC_WRITE, windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE, true},
		{"exclusive", windows.GENERIC_READ | windows.GENERIC_WRITE | windows.DELETE, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := root(t)
			work, err := platform.CreateBackupWorkDirectory(context.Background(), dir)
			if errors.Is(err, platform.ErrBackupWorkDirectoryUnsupported) {
				t.Skipf("work directory unavailable: %v", err)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer work.Close()
			source, err := sql.Open("sqlite", filepath.Join(dir, "source.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			source.SetMaxOpenConns(1)
			for _, statement := range []string{"CREATE TABLE works(id INTEGER PRIMARY KEY,title TEXT)", "INSERT INTO works VALUES(7,'合成作品')"} {
				if _, err := source.Exec(statement); err != nil {
					t.Fatal(err)
				}
			}
			out, err := create(filepath.Join(work.Path(), "copy.sqlite"), tc.access, tc.share)
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			before := id(t, out)
			_, err = source.Exec("VACUUM INTO ?", out.Name())
			if (err == nil) != tc.works {
				t.Fatalf("unexpected copy outcome: %v", err)
			}
			if after := id(t, out); after != before {
				t.Fatal("output identity changed")
			}
			if err := os.Rename(out.Name(), out.Name()+"-moved"); err == nil {
				t.Fatal("retained output renamed")
			}
			if !tc.works {
				t.Logf("copy rejected with retained identity: %v", err)
				return
			}
			copyDB, err := sql.Open("sqlite", out.Name())
			if err != nil {
				t.Fatal(err)
			}
			var title, check string
			readErr := copyDB.QueryRow("SELECT title FROM works WHERE id=7").Scan(&title)
			checkErr := copyDB.QueryRow("PRAGMA quick_check").Scan(&check)
			if err := errors.Join(readErr, checkErr, copyDB.Close()); err != nil {
				t.Fatal(err)
			}
			if title != "合成作品" || check != "ok" {
				t.Fatal(title, check)
			}
			t.Log("copy succeeded, same ID/content/quick_check and rename refusal verified")
			// A single documented same-object handle candidate. No close/reopen by
			// path, ACL change, privilege adjustment, or native API fallback is tried.
			var duplicate windows.Handle
			proc := windows.CurrentProcess()
			if err := windows.DuplicateHandle(proc, windows.Handle(out.Fd()), proc, &duplicate, windows.DELETE|windows.FILE_READ_ATTRIBUTES, false, 0); err != nil {
				t.Logf("same-object DELETE handle unavailable; cleanup NOT verified: %v", err)
				return
			}
			cleanup := os.NewFile(uintptr(duplicate), "duplicate")
			defer cleanup.Close()
			if actual := id(t, cleanup); actual != before {
				t.Fatal("duplicate identity mismatch")
			}
			remove := byte(1)
			if err := windows.SetFileInformationByHandle(duplicate, windows.FileDispositionInfo, &remove, 1); err != nil {
				t.Logf("same-object disposition rejected; cleanup NOT verified: %v", err)
				return
			}
			if err := errors.Join(cleanup.Close(), out.Close()); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(out.Name()); !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
				t.Fatal("output absence not confirmed", err)
			}
			r, err := work.DiscardEmpty()
			if err != nil || !r.MarkedForDeletion || !r.Absent {
				t.Fatal(r, err)
			}
			t.Log("same-object duplicate DELETE disposition, file absence and empty-work cleanup verified")
		})
	}
}
