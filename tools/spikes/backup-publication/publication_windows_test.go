//go:build windows

// Package publication contains a disposable Windows API probe, not a product
// file publisher. All mutations target t.TempDir fixtures only.
package publication

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"github.com/asam-masa/manga-update-manager/internal/backup"
	"golang.org/x/sys/windows"
)

type identity struct {
	Volume uint64
	ID     [16]byte
}

func fileID(f *os.File) (identity, error) {
	var id identity
	err := windows.GetFileInformationByHandleEx(windows.Handle(f.Fd()), windows.FileIdInfo, (*byte)(unsafe.Pointer(&id)), uint32(unsafe.Sizeof(id)))
	return id, err
}

func openFile(name string, access, creation uint32) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, access, 0, nil, creation, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), name), nil
}

// Keep every existing ancestor from being renamed and reject reparse points.
// This tests the repository's current guard idea, not all path/drive attacks.
func holdDirectory(name string) (release func() error, err error) {
	var names []string
	for p := name; ; p = filepath.Dir(p) {
		names = append(names, p)
		if filepath.Dir(p) == p {
			break
		}
	}
	var handles []windows.Handle
	release = func() error {
		var result error
		for i := len(handles) - 1; i >= 0; i-- {
			result = errors.Join(result, windows.CloseHandle(handles[i]))
		}
		handles = nil
		return result
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, release())
		}
	}()
	for i := len(names) - 1; i >= 0; i-- {
		p, e := windows.UTF16PtrFromString(names[i])
		if e != nil {
			return release, e
		}
		h, e := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if e != nil {
			return release, e
		}
		handles = append(handles, h)
		var info windows.ByHandleFileInformation
		if e = windows.GetFileInformationByHandle(h, &info); e != nil {
			return release, e
		}
		if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
			return release, errors.New("reparse or non-directory")
		}
	}
	return release, nil
}

// FILE_RENAME_INFO's union remains zero: ReplaceIfExists=FALSE. Use the held
// source handle and an absolute target under the retained fixture directory.
func renameNoReplace(f *os.File, target string) error {
	return renameTarget(f, target, 0)
}

func renameTarget(f *os.File, target string, root windows.Handle) error {
	name, err := windows.UTF16FromString(target)
	if err != nil {
		return err
	}
	type renameInfo struct {
		Flags  uint32
		Root   windows.Handle
		Length uint32
		Name   [1]uint16
	}
	var header renameInfo
	offset := unsafe.Offsetof(header.Name)
	b := make([]byte, int(offset)+2*len(name))
	h := (*renameInfo)(unsafe.Pointer(&b[0]))
	h.Root = root
	h.Length = uint32(2 * (len(name) - 1))
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(&b[offset])), len(name)), name)
	return windows.SetFileInformationByHandle(windows.Handle(f.Fd()), windows.FileRenameInfo, &b[0], uint32(len(b)))
}

func finalName(f *os.File) (string, error) {
	b := make([]uint16, 32768)
	n, err := windows.GetFinalPathNameByHandle(windows.Handle(f.Fd()), &b[0], uint32(len(b)), 0)
	if err != nil {
		return "", err
	}
	if n >= uint32(len(b)) {
		return "", errors.New("path buffer too small")
	}
	return strings.TrimPrefix(windows.UTF16ToString(b[:n]), `\\?\`), nil
}

func deleteOwned(f *os.File, want identity) error {
	got, err := fileID(f)
	if err != nil {
		return err
	}
	if got != want {
		return errors.New("identity mismatch: preserve")
	}
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info); err != nil {
		return err
	}
	if info.NumberOfLinks != 1 || info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 {
		return errors.New("unsafe deletion target")
	}
	flag := byte(1)
	return windows.SetFileInformationByHandle(windows.Handle(f.Fd()), windows.FileDispositionInfo, &flag, 1)
}

type fixture struct {
	dir, temp, final string
	file             *os.File
	id               identity
	data             []byte
}

func setup(t *testing.T) *fixture {
	return setupAt(t, t.TempDir())
}

func setupAt(t *testing.T, root string) *fixture {
	t.Helper()
	dir := filepath.Join(root, "save")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	release, err := holdDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := release(); err != nil {
			t.Error(err)
		}
	})
	f := &fixture{dir: dir, temp: filepath.Join(dir, "probe.partial"), final: filepath.Join(dir, "probe.zip"), data: []byte("synthetic backup bytes")}
	f.file, err = openFile(f.temp, windows.GENERIC_READ|windows.GENERIC_WRITE|windows.DELETE, windows.CREATE_NEW)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeFixture(t, f) })
	// Temp paths can contain DOS 8.3 aliases (e.g. a CI user profile). Compare
	// names in the representation returned for this newly created handle.
	// This is fixture normalization, not a product path acceptance policy.
	f.temp, err = finalName(f.file)
	if err != nil {
		t.Fatal(err)
	}
	f.dir = filepath.Dir(f.temp)
	f.final = filepath.Join(f.dir, "probe.zip")
	f.id, err = fileID(f.file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.file.Write(f.data); err != nil {
		t.Fatal(err)
	}
	if err = f.file.Sync(); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(f.data))
	if _, err = f.file.ReadAt(got, 0); err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(got) != sha256.Sum256(f.data) {
		t.Fatal("readback mismatch")
	}
	return f
}
func closeFixture(t *testing.T, f *fixture) {
	t.Helper()
	if f.file != nil {
		file := f.file
		f.file = nil
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	}
}
func content(t *testing.T, name string, want []byte) {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil || !bytes.Equal(b, want) {
		t.Fatalf("content mismatch: %q %v", b, err)
	}
}

func TestHandlePublicationAndIdentity(t *testing.T) {
	f := setup(t)
	var fsName [64]uint16
	if err := windows.GetVolumeInformationByHandle(windows.Handle(f.file.Fd()), nil, 0, nil, nil, nil, &fsName[0], uint32(len(fsName))); err != nil {
		t.Fatal(err)
	}
	v := windows.RtlGetVersion()
	t.Logf("Windows %d.%d.%d %s, Go %s, FS %s", v.MajorVersion, v.MinorVersion, v.BuildNumber, runtime.GOARCH, runtime.Version(), windows.UTF16ToString(fsName[:]))
	if err := renameNoReplace(f.file, f.final); err != nil {
		t.Fatal(err)
	}
	id, err := fileID(f.file)
	if err != nil || id != f.id {
		t.Fatal(id, err)
	}
	name, err := finalName(f.file)
	if err != nil || !strings.EqualFold(name, f.final) {
		t.Fatal(name, err)
	}
	if err = f.file.Sync(); err != nil {
		t.Fatal(err)
	}
	closeFixture(t, f)
	content(t, f.final, f.data)
	if _, err = os.Lstat(f.temp); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	r, err := os.Open(f.final)
	if err != nil {
		t.Fatal(err)
	}
	id, err = fileID(r)
	closeErr := r.Close()
	if err != nil || closeErr != nil || id != f.id {
		t.Fatal(id, err, closeErr)
	}
}

func TestExistingAndLateDestinationArePreserved(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing", true: "created-after-check"}[late], func(t *testing.T) {
			f := setup(t)
			if late {
				if _, err := os.Lstat(f.final); !errors.Is(err, os.ErrNotExist) {
					t.Fatal(err)
				}
			}
			old := []byte("existing unrelated backup")
			if err := os.WriteFile(f.final, old, 0600); err != nil {
				t.Fatal(err)
			}
			err := renameNoReplace(f.file, f.final)
			if !errors.Is(err, windows.ERROR_ALREADY_EXISTS) && !errors.Is(err, windows.ERROR_FILE_EXISTS) {
				t.Fatalf("expected collision error, got %v", err)
			}
			t.Logf("collision rejected: %v", err)
			name, e := finalName(f.file)
			if e != nil || !strings.EqualFold(name, f.temp) {
				t.Fatal(name, e)
			}
			closeFixture(t, f)
			content(t, f.final, old)
			content(t, f.temp, f.data)
		})
	}
}

func TestHeldSourceAndDirectoryCannotBeReplaced(t *testing.T) {
	f := setup(t)
	if other, err := openFile(f.temp, windows.GENERIC_WRITE, windows.OPEN_EXISTING); err == nil {
		other.Close()
		t.Fatal("exclusive source reopened for write")
	}
	if err := os.Rename(f.temp, filepath.Join(f.dir, "moved.partial")); err == nil {
		t.Fatal("held source renamed")
	}
	if err := os.Rename(f.dir, filepath.Join(filepath.Dir(f.dir), "moved-save")); err == nil {
		t.Fatal("held directory renamed")
	}
	if another, err := openFile(f.temp, windows.GENERIC_WRITE, windows.CREATE_NEW); err == nil {
		another.Close()
		t.Fatal("existing temp replaced")
	}
}

func TestOwnedCleanupAndMismatchPreservation(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(map[bool]string{false: "owned", true: "mismatch"}[mismatch], func(t *testing.T) {
			f := setup(t)
			id := f.id
			if mismatch {
				id.ID[0] ^= 1
			}
			err := deleteOwned(f.file, id)
			if (err != nil) != mismatch {
				t.Fatal(err)
			}
			closeFixture(t, f)
			if mismatch {
				content(t, f.temp, f.data)
			} else if _, err := os.Lstat(f.temp); !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
		})
	}
}

func TestReparseDirectoryIsRejected(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink creation unavailable; no settings changed: %v", err)
	}
	release, err := holdDirectory(link)
	if err == nil {
		release()
		t.Fatal("reparse directory accepted")
	}
}

type phase int

const (
	notStarted phase = iota
	published
	unknown
)

// State model only: no product orchestration or cancellation UI is connected.
func commit(ctx context.Context, operation func() error) (phase, error) {
	if err := ctx.Err(); err != nil {
		return notStarted, err
	}
	if err := operation(); err != nil {
		return unknown, err
	}
	return published, nil // A late cancellation must not roll back completed output.
}

func TestCancelBoundaryAndUnknownPreservation(t *testing.T) {
	for _, scenario := range []string{"before", "after", "unknown", "unknown-before-change"} {
		t.Run(scenario, func(t *testing.T) {
			f := setup(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "before" {
				cancel()
			}
			called := false
			cause := errors.New("synthetic lost result")
			state, err := commit(ctx, func() error {
				called = true
				if scenario == "unknown-before-change" {
					return cause
				}
				if err := renameNoReplace(f.file, f.final); err != nil {
					return err
				}
				cancel()
				if scenario == "unknown" {
					return cause
				}
				return nil
			})
			switch scenario {
			case "before":
				if called || state != notStarted || !errors.Is(err, context.Canceled) {
					t.Fatal(state, err, called)
				}
				if err = deleteOwned(f.file, f.id); err != nil {
					t.Fatal(err)
				}
			case "after":
				if state != published || err != nil {
					t.Fatal(state, err)
				}
			case "unknown", "unknown-before-change":
				if state != unknown || !errors.Is(err, cause) {
					t.Fatal(state, err)
				}
			}
			closeFixture(t, f)
			if scenario == "unknown-before-change" {
				content(t, f.temp, f.data)
				if _, err = os.Lstat(f.final); !errors.Is(err, os.ErrNotExist) {
					t.Fatal(err)
				}
			} else if scenario != "before" {
				content(t, f.final, f.data)
			} else if _, err = os.Lstat(f.final); !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
		})
	}
}

func TestReadOnlyHandleCannotSyncOrDelete(t *testing.T) {
	f := setup(t)
	closeFixture(t, f)
	r, err := openFile(f.temp, windows.GENERIC_READ, windows.OPEN_EXISTING)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Sync(); err == nil {
		r.Close()
		t.Fatal("read-only handle unexpectedly flushed")
	}
	err = deleteOwned(r, f.id)
	closeErr := r.Close()
	if err == nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	content(t, f.temp, f.data)
}

func TestValidatedZIPPublication(t *testing.T) {
	f := setup(t)
	if err := f.file.Truncate(0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	db := []byte("synthetic database bytes, not SQLite")
	manifest := []byte(fmt.Sprintf(`{"formatVersion":1,"createdAt":"2026-11-03T00:00:00Z","appVersion":"spike","migrations":["001_create_works.sql"],"database":{"path":"database.sqlite","size":%d,"sha256":"%x"},"images":[],"omittedImages":[]}`, len(db), sha256.Sum256(db)))
	if err := backup.WriteArchive(context.Background(), f.file, manifest, func(name string) (io.ReadCloser, error) {
		if name != "database.sqlite" {
			return nil, errors.New("unexpected source")
		}
		return io.NopCloser(bytes.NewReader(db)), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.file.Sync(); err != nil {
		t.Fatal(err)
	}
	stat, err := f.file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = backup.ValidateArchive(f.file, stat.Size()); err != nil {
		t.Fatal(err)
	}
	if err = renameNoReplace(f.file, f.final); err != nil {
		t.Fatal(err)
	}
	id, err := fileID(f.file)
	if err != nil || id != f.id {
		t.Fatal(id, err)
	}
	if err = f.file.Sync(); err != nil {
		t.Fatal(err)
	}
	closeFixture(t, f)
	r, err := os.Open(f.final)
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := backup.ValidateArchive(r, stat.Size())
	id, idErr := fileID(r)
	closeErr := r.Close()
	if readErr != nil || idErr != nil || closeErr != nil || id != f.id {
		t.Fatal(readErr, idErr, closeErr, id)
	}
}

func TestDirectoryRelativeRenameProbe(t *testing.T) {
	f := setup(t)
	p, err := windows.UTF16PtrFromString(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := windows.CloseHandle(h); err != nil {
			t.Error(err)
		}
	}()
	err = renameTarget(f.file, "relative.zip", h)
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		t.Logf("RootDirectory + relative name unavailable on this host: %v", err)
		closeFixture(t, f)
		content(t, f.temp, f.data)
		if _, e := os.Lstat(filepath.Join(f.dir, "relative.zip")); !errors.Is(e, os.ErrNotExist) {
			t.Fatal(e)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Log("RootDirectory + relative name accepted on this host; not a cross-version guarantee")
	closeFixture(t, f)
	content(t, filepath.Join(f.dir, "relative.zip"), f.data)
}

func TestShortPathFixture(t *testing.T) {
	root := t.TempDir()
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]uint16, 32768)
	n, err := windows.GetShortPathName(p, &b[0], uint32(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	if n >= uint32(len(b)) {
		t.Fatal("short path buffer too small")
	}
	alias := windows.UTF16ToString(b[:n])
	long := make([]uint16, 32768)
	longN, err := windows.GetLongPathName(p, &long[0], uint32(len(long)))
	if err != nil || longN >= uint32(len(long)) {
		t.Fatal("long path conversion", longN, err)
	}
	if strings.EqualFold(alias, windows.UTF16ToString(long[:longN])) {
		t.Skip("8.3 alias unavailable; volume settings unchanged")
	}
	f := setupAt(t, alias)
	if err := renameNoReplace(f.file, f.final); err != nil {
		t.Fatal(err)
	}
	name, err := finalName(f.file)
	if err != nil || !strings.EqualFold(name, f.final) {
		t.Fatal(name, f.final, err)
	}
	id, err := fileID(f.file)
	if err != nil || id != f.id {
		t.Fatal(id, err)
	}
	closeFixture(t, f)
	content(t, f.final, f.data)
}
