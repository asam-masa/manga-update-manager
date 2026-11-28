package platform

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestOpenRestoreRecordChild(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	activePath := filepath.Join(root, "restore-active")
	if err := os.Mkdir(activePath, 0700); err != nil {
		t.Fatal(err)
	}
	parent := openRestoreTestDirectory(t, root)
	otherParent := openRestoreTestDirectory(t, other)

	active := requireRestoreTestChild(t, parent, "restore-active")
	if _, missing, err := openRestoreRecordChild(otherParent, "restore-active"); !missing || err != nil {
		t.Fatalf("same name under another parent: missing=%v err=%v", missing, err)
	}

	plan := filepath.Join(activePath, "plan.json")
	writer, err := os.Create(plan)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	if _, missing, err := openRestoreRecordChild(active, "plan.json"); missing || !errors.Is(err, windows.STATUS_SHARING_VIOLATION) {
		t.Fatalf("open with active writer: missing=%v err=%v", missing, err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	requireRestoreTestChild(t, active, "plan.json")

	if err := os.Mkdir(filepath.Join(activePath, "complete.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, missing, err := openRestoreRecordChild(active, "complete.json"); missing || !errors.Is(err, ErrUnsafeBackupDirectory) {
		t.Fatalf("directory at record name: missing=%v err=%v", missing, err)
	}
	if _, missing, err := openRestoreRecordChild(active, "../plan.json"); missing || err == nil {
		t.Fatalf("relative path instead of fixed name: missing=%v err=%v", missing, err)
	}
}

func TestOpenRestoreRecordChildRejectsReparsePoint(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.json")
	if err := os.WriteFile(target, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "plan.json")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	parent := openRestoreTestDirectory(t, root)
	handle, missing, err := openRestoreRecordChild(parent, "plan.json")
	if handle != 0 || missing || err == nil {
		if handle != 0 {
			_ = windows.CloseHandle(handle)
		}
		t.Fatalf("reparse point: handle=%v missing=%v err=%v", handle, missing, err)
	}
}

func openRestoreTestDirectory(t *testing.T, path string) windows.Handle {
	t.Helper()
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := windows.CloseHandle(handle); err != nil {
			t.Error(err)
		}
	})
	if err := checkBackupDirectory(handle); err != nil {
		t.Fatal(err)
	}
	return handle
}

func requireRestoreTestChild(t *testing.T, parent windows.Handle, name string) windows.Handle {
	t.Helper()
	handle, missing, err := openRestoreRecordChild(parent, name)
	if missing || err != nil {
		t.Fatalf("open %s: missing=%v err=%v", name, missing, err)
	}
	t.Cleanup(func() {
		if err := windows.CloseHandle(handle); err != nil {
			t.Error(err)
		}
	})
	return handle
}
