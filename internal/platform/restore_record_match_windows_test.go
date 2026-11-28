package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestMatchRestoreRecordChild(t *testing.T) {
	path := t.TempDir()
	if err := os.WriteFile(filepath.Join(path, "plan.json"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "Complete.json"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	parent := openRestoreTestDirectory(t, path)
	ctx := context.Background()

	child, missing, err := matchRestoreRecordChild(ctx, parent, "plan.json")
	if err != nil || missing || child == 0 {
		t.Fatalf("present record: child=%v missing=%v err=%v", child, missing, err)
	}
	defer func() {
		if err := windows.CloseHandle(child); err != nil {
			t.Error(err)
		}
	}()
	id, err := restoreRecordHandleIdentity(child)
	if err != nil {
		t.Fatal(err)
	}
	id.FileID[0] ^= 1
	if err := matchOpenRestoreRecordChild(parent, child, restoreRecordDirectoryEntry{Name: "plan.json", FileID: id.FileID}); !errors.Is(err, errRestoreRecordMismatch) {
		t.Fatalf("mismatched ID: %v", err)
	}

	for _, test := range []struct {
		name    string
		missing bool
		err     error
	}{
		{name: "restore-active", missing: true},
		{name: "complete.json", err: errRestoreRecordMismatch},
	} {
		handle, missing, err := matchRestoreRecordChild(ctx, parent, test.name)
		if handle != 0 {
			_ = windows.CloseHandle(handle)
		}
		if handle != 0 || missing != test.missing || !errors.Is(err, test.err) {
			t.Errorf("%s: child=%v missing=%v err=%v", test.name, handle, missing, err)
		}
	}
}
