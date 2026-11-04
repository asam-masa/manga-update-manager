package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/sys/windows"
)

func TestCreateBackupTempRetainsIdentityAndParents(t *testing.T) {
	dir := t.TempDir()
	final := filepath.Join(dir, "保存.zip")
	if err := os.WriteFile(final, []byte("keep final"), 0600); err != nil {
		t.Fatal(err)
	}
	temp, err := CreateBackupTemp(context.Background(), final)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := temp.Close(); err != nil {
			t.Error(err)
		}
	})
	if filepath.Dir(temp.Path()) != dir || temp.Path() == final {
		t.Fatal("wrong temp location")
	}
	id, err := identifyBackupTemp(temp.file)
	if err != nil || id != temp.Identity() {
		t.Fatalf("identity mismatch: %v", err)
	}
	p, err := windows.UTF16PtrFromString(temp.Path())
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, 0, 0)
	if err == nil {
		windows.CloseHandle(h)
		t.Fatal("reopened held temp for write")
	}
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("unexpected reopen error: %v", err)
	}
	if err := os.Rename(dir, dir+"-moved"); err == nil {
		t.Fatal("renamed held parent")
	}
	if err := temp.Close(); err != nil {
		t.Fatal(err)
	}
	if err := temp.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(temp.Path())
	if err != nil {
		t.Fatal(err)
	}
	id, idErr := identifyBackupTemp(f)
	closeErr := f.Close()
	if idErr != nil || closeErr != nil || id != temp.Identity() {
		t.Fatalf("identity after Close: %v %v", idErr, closeErr)
	}
	data, err := os.ReadFile(final)
	if err != nil || string(data) != "keep final" {
		t.Fatalf("final changed: %q %v", data, err)
	}
	if err := os.Rename(dir, dir+"-moved"); err != nil {
		t.Fatalf("parent retained after Close: %v", err)
	}
}

func TestCreateBackupTempCollisionDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	final := filepath.Join(dir, "backup.zip")
	var token [16]byte
	first, err := createBackupTemp(context.Background(), final, token, identifyBackupTemp)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first.Path(), []byte("keep temp"), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := createBackupTemp(context.Background(), final, token, identifyBackupTemp)
	var detail *BackupTempError
	if second != nil || !errors.As(err, &detail) || detail.Created || detail.Path != first.Path() || !errors.Is(err, windows.ERROR_FILE_EXISTS) {
		t.Fatalf("unexpected collision: %v %v", second, err)
	}
	data, err := os.ReadFile(first.Path())
	if err != nil || string(data) != "keep temp" {
		t.Fatalf("temp changed: %q %v", data, err)
	}
	if same, err := createBackupTemp(context.Background(), first.Path(), token, identifyBackupTemp); same != nil || !errors.Is(err, ErrInvalidBackupDestination) {
		t.Fatalf("accepted matching final and temp: %v %v", same, err)
	}
}

func TestCreateBackupTempPreservesCreatedFileOnFailure(t *testing.T) {
	for _, cancelAfterCreate := range []bool{false, true} {
		dir := t.TempDir()
		ctx, cancel := context.WithCancel(context.Background())
		failure := errors.New("synthetic identity failure")
		identify := func(file *os.File) (BackupTempIdentity, error) {
			if cancelAfterCreate {
				cancel()
				return identifyBackupTemp(file)
			}
			return BackupTempIdentity{}, failure
		}
		temp, err := createBackupTemp(ctx, filepath.Join(dir, "backup.zip"), [16]byte{}, identify)
		cancel()
		var detail *BackupTempError
		want := failure
		if cancelAfterCreate {
			want = context.Canceled
		}
		if temp != nil || !errors.As(err, &detail) || !detail.Created || !errors.Is(err, want) {
			t.Fatalf("failure lost: %v %v", temp, err)
		}
		info, statErr := os.Stat(detail.Path)
		if statErr != nil || info.Size() != 0 {
			t.Fatalf("created output lost: %v", statErr)
		}
		if err := os.Rename(dir, dir+"-moved"); err != nil {
			t.Fatalf("resources retained: %v", err)
		}
	}
}

type backupTestCloser func() error

func (f backupTestCloser) Close() error { return f() }

func TestBackupTempCloseAttemptsFileThenDirectoryOnce(t *testing.T) {
	failure := errors.New("synthetic file Close failure")
	dirFailure := errors.New("synthetic directory Close failure")
	var order []string
	temp := &BackupTemp{
		closeFile: func() error { order = append(order, "file"); return failure },
		directory: backupTestCloser(func() error { order = append(order, "directory"); return dirFailure }),
	}
	for i := 0; i < 2; i++ {
		err := temp.Close()
		if !errors.Is(err, ErrBackupTempRelease) || !errors.Is(err, failure) || !errors.Is(err, dirFailure) {
			t.Fatalf("lost error: %v", err)
		}
	}
	if !reflect.DeepEqual(order, []string{"file", "directory"}) {
		t.Fatalf("close order/count: %v", order)
	}
}

func TestCreateBackupTempCancelledBeforeCreation(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	temp, err := CreateBackupTemp(ctx, filepath.Join(dir, "backup.zip"))
	if temp != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel not preserved: %v %v", temp, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("cancel created output: %v %v", entries, err)
	}
}
