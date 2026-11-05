package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func workDirectoryOps(t *testing.T) backupWorkDirectoryOps {
	t.Helper()
	if err := createBackupDirectoryProc.Find(); err != nil {
		t.Skipf("positive capability test: %v", err)
	}
	return backupWorkDirectoryOps{createBackupDirectoryProc.Find, createBackupDirectoryHandle, identifyBackupWorkDirectory, backupHandleName}
}

// CI's TEMP may contain an 8.3 alias. Positive fixtures use the same normalized
// DOS spelling required by the boundary; aliases have a separate refusal test.
func workDirectoryRoot(t *testing.T) string {
	t.Helper()
	f, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	name, nameErr := backupHandleName(f)
	if err := errors.Join(nameErr, f.Close()); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestBackupWorkDirectoryLifetimeAndCollision(t *testing.T) {
	ops := workDirectoryOps(t)
	root := workDirectoryRoot(t)
	dir := filepath.Join(root, "日本語 作業")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	d, err := createBackupWorkDirectory(context.Background(), dir, [16]byte{}, ops)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.VerifyIdentity(); err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(d.Path()) != dir || !strings.HasPrefix(filepath.Base(d.Path()), ".manga-update-backup-db-") {
		t.Fatal(d.Path())
	}
	for _, name := range []string{dir, d.Path()} {
		if err := os.Rename(name, name+"-moved"); err == nil {
			t.Fatal("held directory moved")
		}
	}
	if other, err := createBackupWorkDirectory(context.Background(), dir, [16]byte{}, ops); other != nil || err == nil {
		t.Fatal("existing directory reused", err)
	} else {
		var detail *BackupWorkDirectoryError
		if !errors.As(err, &detail) || detail.Created || !errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, ErrBackupWorkDirectoryUnsupported) {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(d.Path(), "retained.db"), []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	d.identity.FileID[0] ^= 0xff
	if err := d.VerifyIdentity(); !errors.Is(err, ErrBackupWorkDirectoryIdentity) {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := d.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.VerifyIdentity(); !errors.Is(err, os.ErrClosed) {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(d.Path(), "retained.db")); err != nil || string(b) != "synthetic" {
		t.Fatal("Close deleted data", err)
	}
	if err := os.Rename(dir, dir+"-moved"); err != nil {
		t.Fatal("parent retained after Close", err)
	}
}

func TestBackupWorkDirectoryPostCreationFailures(t *testing.T) {
	for _, kind := range []string{"identity", "name", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			ops := workDirectoryOps(t)
			dir := workDirectoryRoot(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			failure := errors.New("synthetic inspection failure")
			want := failure
			switch kind {
			case "identity":
				ops.identify = func(*os.File) (BackupWorkDirectoryIdentity, error) { return BackupWorkDirectoryIdentity{}, failure }
			case "name":
				ops.name = func(*os.File) (string, error) { return filepath.Join(dir, "different"), nil }
				want = ErrBackupWorkDirectoryIdentity
			case "cancel":
				ops.identify = func(f *os.File) (BackupWorkDirectoryIdentity, error) {
					id, err := identifyBackupWorkDirectory(f)
					cancel()
					return id, err
				}
				want = context.Canceled
			}
			d, err := createBackupWorkDirectory(ctx, dir, [16]byte{}, ops)
			var detail *BackupWorkDirectoryError
			if d != nil || !errors.Is(err, want) || !errors.As(err, &detail) || !detail.Created {
				t.Fatal(d, err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 || filepath.Join(dir, entries[0].Name()) != detail.Path {
				t.Fatal("created output not retained", entries, err)
			}
			if err := os.Rename(dir, dir+"-moved"); err != nil {
				t.Fatal("failed creation leaked handles", err)
			}
		})
	}
}

func TestBackupWorkDirectoryEarlyRefusals(t *testing.T) {
	dir := t.TempDir()
	called := false
	ops := backupWorkDirectoryOps{find: func() error { return windows.ERROR_PROC_NOT_FOUND }, create: func(string) (*os.File, error) { called = true; return nil, nil }}
	if d, err := createBackupWorkDirectory(context.Background(), dir, [16]byte{}, ops); d != nil || !errors.Is(err, ErrBackupWorkDirectoryUnsupported) {
		t.Fatal(d, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if d, err := createBackupWorkDirectory(ctx, dir, [16]byte{}, ops); d != nil || !errors.Is(err, context.Canceled) {
		t.Fatal(d, err)
	}
	if d, err := createBackupWorkDirectory(context.Background(), dir+`\..\other`, [16]byte{}, ops); d != nil || err == nil {
		t.Fatal("normalized invalid path", err)
	}
	entries, err := os.ReadDir(dir)
	if called || err != nil || len(entries) != 0 {
		t.Fatal("refusal caused output", called, entries, err)
	}
}

func TestBackupWorkDirectoryCloseErrorsCached(t *testing.T) {
	failure, parentFailure := errors.New("child close"), errors.New("parent close")
	order := ""
	d := &BackupWorkDirectory{closeFile: func() error { order += "child;"; return failure }, parent: backupTestCloser(func() error { order += "parent;"; return parentFailure })}
	for i := 0; i < 2; i++ {
		err := d.Close()
		if !errors.Is(err, ErrBackupWorkDirectoryRelease) || !errors.Is(err, failure) || !errors.Is(err, parentFailure) {
			t.Fatal(err)
		}
	}
	if order != "child;parent;" {
		t.Fatal(order)
	}
}

func TestBackupWorkDirectoryRejectsShortAlias(t *testing.T) {
	ops := workDirectoryOps(t)
	dir := workDirectoryRoot(t)
	input, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]uint16, 32768)
	n, err := windows.GetShortPathName(input, &buf[0], uint32(len(buf)))
	if err != nil {
		t.Skipf("short alias unavailable: %v", err)
	}
	if n == 0 || n >= uint32(len(buf)) {
		t.Fatal("invalid alias length")
	}
	short := windows.UTF16ToString(buf[:n])
	if strings.EqualFold(short, dir) {
		t.Skip("volume did not provide a distinct short alias")
	}
	d, err := createBackupWorkDirectory(context.Background(), short, [16]byte{}, ops)
	var detail *BackupWorkDirectoryError
	if d != nil || !errors.Is(err, ErrBackupWorkDirectoryIdentity) || !errors.As(err, &detail) || !detail.Created {
		t.Fatal(d, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatal("alias failure removed created directory", entries, err)
	}
	if err := os.Rename(dir, dir+"-moved"); err != nil {
		t.Fatal("alias refusal retained handles", err)
	}
}
