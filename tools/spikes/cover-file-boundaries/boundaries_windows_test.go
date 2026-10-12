package coverfileboundaries

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// This is a synthetic Windows API probe, not production file authorization.
func openProbe(name string, access, disposition uint32) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return windows.InvalidHandle, err
	}
	return windows.CreateFile(p, access, 0, nil, disposition,
		windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
}

func ordinaryFile(h windows.Handle) (bool, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return false, err
	}
	kind, err := windows.GetFileType(h)
	return err == nil && kind == windows.FILE_TYPE_DISK && info.NumberOfLinks == 1 &&
		info.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) == 0, err
}

func TestCreateNewDoesNotOverwrite(t *testing.T) {
	name := filepath.Join(t.TempDir(), "synthetic.png")
	if err := os.WriteFile(name, []byte("original synthetic bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	h, err := openProbe(name, windows.GENERIC_WRITE, windows.CREATE_NEW)
	if err == nil {
		windows.CloseHandle(h)
		t.Fatal("existing file accepted")
	}
	if !errors.Is(err, windows.ERROR_FILE_EXISTS) {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := os.ReadFile(name)
	if err != nil || string(got) != "original synthetic bytes" {
		t.Fatalf("changed existing file: %q %v", got, err)
	}
}

func TestNewFileWriteFlushAndClose(t *testing.T) {
	name := filepath.Join(t.TempDir(), "new-synthetic.png")
	h, err := openProbe(name, windows.GENERIC_WRITE, windows.CREATE_NEW)
	if err != nil {
		t.Fatal(err)
	}
	file := os.NewFile(uintptr(h), name)
	defer file.Close()
	want := []byte("complete synthetic payload, not a PNG decoder test")
	n, err := file.Write(want)
	if err != nil || n != len(want) {
		t.Fatalf("write: %d %v", n, err)
	}
	if err := file.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(name)
	if err != nil || string(got) != string(want) {
		t.Fatalf("readback: %q %v", got, err)
	}
}

func TestHeldDirectoryBlocksItsRename(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "covers")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	h, err := openProbe(dir, windows.GENERIC_READ, windows.OPEN_EXISTING)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if h != windows.InvalidHandle {
			windows.CloseHandle(h)
		}
	}()
	if err := os.Rename(dir, filepath.Join(root, "moved")); err == nil {
		t.Fatal("held directory renamed")
	}
	if err := os.Remove(dir); err == nil {
		t.Fatal("held directory removed")
	}
	if err := windows.CloseHandle(h); err != nil {
		t.Fatal(err)
	}
	h = windows.InvalidHandle
	moved := filepath.Join(root, "moved")
	if err := os.Rename(dir, moved); err != nil {
		t.Fatalf("rename after release: %v", err)
	}
	if err := os.Remove(moved); err != nil {
		t.Fatalf("remove after release: %v", err)
	}
}

func TestCheckedHandleBlocksReplacementAndDeletesSameFile(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "synthetic.png")
	if err := os.WriteFile(name, []byte("owned synthetic bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	h, err := openProbe(name, windows.GENERIC_READ|windows.DELETE, windows.OPEN_EXISTING)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if h != windows.InvalidHandle {
			windows.CloseHandle(h)
		}
	}()
	ok, err := ordinaryFile(h)
	if err != nil || !ok {
		t.Fatalf("ordinary file: %v %v", ok, err)
	}
	if err := os.Rename(name, filepath.Join(dir, "moved.png")); !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("rename must fail with sharing violation: %v", err)
	}
	if err := os.Remove(name); !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("remove must fail with sharing violation: %v", err)
	}
	if err := os.WriteFile(name, []byte("replacement"), 0600); !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("write must fail with sharing violation: %v", err)
	}
	// FILE_DISPOSITION_INFO has one BOOLEAN byte, not a Win32 BOOL DWORD.
	deleteFile := byte(1)
	if err := windows.SetFileInformationByHandle(h, windows.FileDispositionInfo, &deleteFile, 1); err != nil {
		t.Fatal(err)
	}
	if err := windows.CloseHandle(h); err != nil {
		t.Fatal(err)
	}
	h = windows.InvalidHandle
	if _, err := os.Lstat(name); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("not deleted: %v", err)
	}
}

func TestRejectLinksAndDirectoriesWithoutChangingTargets(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "original.png")
	if err := os.WriteFile(original, []byte("preserved original"), 0600); err != nil {
		t.Fatal(err)
	}
	hard := filepath.Join(dir, "hard.png")
	if err := os.Link(original, hard); err != nil {
		t.Fatal(err)
	}
	sym := filepath.Join(dir, "symbolic.png")
	if err := os.Symlink(original, sym); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{hard, sym, dir} {
		h, err := openProbe(name, windows.GENERIC_READ, windows.OPEN_EXISTING)
		if err != nil {
			t.Fatal(err)
		}
		ok, err := ordinaryFile(h)
		closeErr := windows.CloseHandle(h)
		if err != nil || closeErr != nil || ok {
			t.Fatalf("unsafe file accepted: %v %v %v", ok, err, closeErr)
		}
	}
	data, err := os.ReadFile(original)
	if err != nil || string(data) != "preserved original" {
		t.Fatalf("target changed: %q %v", data, err)
	}
}
