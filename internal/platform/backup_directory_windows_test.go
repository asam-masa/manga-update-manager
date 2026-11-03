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

func TestHoldBackupDirectoryRetainsParentsAndCreatesNothing(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "日本語 保存")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	guard, err := HoldBackupDirectory(context.Background(), filepath.Join(dir, "backup.zip"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := guard.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, path := range []string{dir, parent} {
		if err := os.Rename(path, path+"-moved"); err == nil {
			t.Fatalf("renamed held folder %s", path)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unexpected output: %v %v", entries, err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dir, dir+"-moved"); err != nil {
		t.Fatalf("retained after Close: %v", err)
	}
}

func TestHoldBackupDirectoryRejectsMissingAndNonDirectory(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{filepath.Join(root, "missing"), file} {
		g, err := HoldBackupDirectory(context.Background(), filepath.Join(directory, "backup.zip"))
		if g != nil || err == nil {
			t.Fatalf("accepted %s: %v %v", directory, g, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing directory changed: %v", err)
	}
	if err := os.Rename(root, root+"-moved"); err != nil {
		t.Fatalf("partial acquisition leaked: %v", err)
	}
}

func TestHoldBackupDirectoryRejectsReparseAncestor(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	link := filepath.Join(root, "link")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	g, err := HoldBackupDirectory(context.Background(), filepath.Join(link, "backup.zip"))
	if g != nil || !errors.Is(err, ErrUnsafeBackupDirectory) {
		t.Fatalf("accepted reparse: %v %v", g, err)
	}
}

func TestBackupDirectoryFailureAndRelease(t *testing.T) {
	failure := errors.New("synthetic failure")
	for _, stage := range []string{"remote", "unknown", "ram", "open", "check", "cancel", "release"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var opened []string
			var closed []windows.Handle
			ops := backupDirectoryOps{
				drive: func(string) uint32 {
					switch stage {
					case "remote":
						return windows.DRIVE_REMOTE
					case "unknown":
						return windows.DRIVE_UNKNOWN
					case "ram":
						return windows.DRIVE_RAMDISK
					}
					return windows.DRIVE_REMOVABLE
				},
				open: func(path string) (windows.Handle, error) {
					opened = append(opened, path)
					if stage == "open" && len(opened) == 3 {
						return 0, failure
					}
					return windows.Handle(len(opened)), nil
				},
				check: func(h windows.Handle) error {
					if stage == "check" && h == 2 {
						return ErrUnsafeBackupDirectory
					}
					if stage == "cancel" && h == 2 {
						cancel()
					}
					return nil
				},
				close: func(h windows.Handle) error {
					closed = append(closed, h)
					if stage == "release" && h == 2 {
						return failure
					}
					return nil
				},
			}
			g, err := holdBackupDirectory(ctx, `C:\one\two\backup.zip`, ops)
			var want []windows.Handle
			switch stage {
			case "remote", "unknown", "ram":
				if len(opened) != 0 || !errors.Is(err, ErrUnsafeBackupDirectory) {
					t.Fatalf("drive not rejected: %v %v", opened, err)
				}
			case "open":
				want = []windows.Handle{2, 1}
				if !errors.Is(err, failure) {
					t.Fatal(err)
				}
			case "check":
				want = []windows.Handle{2, 1}
				if !errors.Is(err, ErrUnsafeBackupDirectory) {
					t.Fatal(err)
				}
			case "cancel":
				want = []windows.Handle{2, 1}
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case "release":
				if err != nil || g == nil {
					t.Fatalf("acquisition: %v", err)
				}
				want = []windows.Handle{3, 2, 1}
				for i := 0; i < 2; i++ {
					e := g.Close()
					if !errors.Is(e, ErrBackupDirectoryRelease) || !errors.Is(e, failure) {
						t.Fatalf("lost release error: %v", e)
					}
				}
			}
			if stage != "release" && g != nil {
				t.Fatal("partial guard returned")
			}
			if !reflect.DeepEqual(closed, want) {
				t.Fatalf("release order/count: %v; want %v", closed, want)
			}
		})
	}
}

func TestBackupDirectoryRejectsOtherOrUnknownFileSystems(t *testing.T) {
	for _, name := range []string{"exFAT", "FAT32", "ReFS", "", "NTFS-other"} {
		if !errors.Is(checkBackupFileSystem(name), ErrUnsafeBackupDirectory) {
			t.Fatalf("accepted filesystem %q", name)
		}
	}
	if err := checkBackupFileSystem("NTFS"); err != nil {
		t.Fatal(err)
	}
}
