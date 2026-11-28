package platform

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

var errRecoveryFixtureIdentity = errors.New("recovery fixture identity mismatch")

// This test adapter combines existing handles. It does not authorize a later
// move: it holds the observation only through the callback.
func withRecoveryFixtureFile(ctx context.Context, root, path string, expected BackupTempIdentity, use func(io.Reader) error) (err error) {
	lock, err := AcquireStorageLock(root)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Close()) }()
	parents, err := HoldBackupDirectory(ctx, path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, parents.Close()) }()
	file, err := openRestoreSourceFile(path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	actual, err := observeRestoreSource(file)
	if err != nil {
		return err
	}
	if actual.id != expected || !strings.EqualFold(actual.name, path) {
		return errRecoveryFixtureIdentity
	}
	return use(file)
}

func TestRecoveryFixtureReacquireUnderStorageLock(t *testing.T) {
	root := workDirectoryRoot(t)
	dir := filepath.Join(root, "retired")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "fixture.sqlite")
	requestParentDeleteAccess := func() error {
		name, err := windows.UTF16PtrFromString(dir)
		if err != nil {
			return err
		}
		handle, err := windows.CreateFile(name, windows.DELETE,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
			nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
		if err != nil {
			return err
		}
		return windows.CloseHandle(handle)
	}
	const payload = "synthetic retired database bytes"
	if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}
	initial, err := observeRestoreFixturePath(path)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	observe := func(r io.Reader) error {
		calls++
		data, err := io.ReadAll(r)
		if err != nil || string(data) != payload {
			t.Fatal(string(data), err)
		}
		other, err := AcquireStorageLock(root)
		if other != nil {
			_ = other.Close()
		}
		if !errors.Is(err, ErrStorageInUse) {
			t.Fatal("storage lock not retained", err)
		}
		writer, err := os.OpenFile(path, os.O_WRONLY, 0)
		if writer != nil {
			_ = writer.Close()
		}
		if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			t.Fatal("held file did not reject a writer", err)
		}
		if err := os.Rename(path, path+".moved"); !errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			t.Fatal("held file did not reject rename", err)
		}
		if err := requestParentDeleteAccess(); !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			t.Fatal("parent handle did not independently deny DELETE access", err)
		}
		if err := os.Rename(dir, dir+".moved"); !errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			t.Fatal("held parent did not reject rename", err)
		}
		return nil
	}

	ctx := context.Background()
	other, err := AcquireStorageLock(root)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := withRecoveryFixtureFile(ctx, root, path, initial.id, observe); !errors.Is(err, ErrStorageInUse) || calls != 0 {
		t.Fatal("competing lock must prevent use", calls, err)
	}
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}
	if err := withRecoveryFixtureFile(ctx, root, path, initial.id, observe); err != nil || calls != 1 {
		t.Fatal(calls, err)
	}
	if err := requestParentDeleteAccess(); err != nil {
		t.Fatal("parent DELETE access after release", err)
	}

	parked := path + ".original"
	if err := os.Rename(path, parked); err != nil {
		t.Fatal("rename after release", err)
	}
	if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}
	if err := withRecoveryFixtureFile(ctx, root, path, initial.id, observe); !errors.Is(err, errRecoveryFixtureIdentity) || calls != 1 {
		t.Fatal("same contents must not prove identity", calls, err)
	}
	for _, name := range []string{path, parked} {
		if data, err := os.ReadFile(name); err != nil || string(data) != payload {
			t.Fatal("fixture changed", name, err)
		}
	}
}
