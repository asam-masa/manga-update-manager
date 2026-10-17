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

func TestPrepareCoverDirectoryLifecycle(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "app")
	if err := os.Mkdir(base, 0700); err != nil {
		t.Fatal(err)
	}
	guard, err := PrepareCoverDirectory(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	cover := filepath.Join(base, "covers")
	if info, err := os.Stat(cover); err != nil || !info.IsDir() {
		t.Fatalf("not created: %v", err)
	}
	name := filepath.Join(cover, "synthetic.txt")
	if err := os.WriteFile(name, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	other, err := PrepareCoverDirectory(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadDir(cover); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{base, cover} {
		err := os.Rename(dir, dir+"-moved")
		if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			t.Fatalf("held rename succeeded: %v", err)
		}
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{cover, base} {
		if err := os.Rename(dir, dir+"-moved"); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(dir+"-moved", dir); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(name)
	if err != nil || string(got) != "preserve" {
		t.Fatal("existing contents changed")
	}
}

func TestPrepareCoverDirectoryRejectsUnsafeAndReleases(t *testing.T) {
	for _, mode := range []string{"parent-link", "cover-link", "parent-file", "cover-file", "missing-parent"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			base := filepath.Join(root, "app")
			target := t.TempDir()
			sentinel := filepath.Join(target, "synthetic.txt")
			if err := os.WriteFile(sentinel, []byte("unchanged"), 0600); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "parent-link":
				if err := os.Symlink(target, base); err != nil {
					t.Fatal(err)
				}
			case "parent-file":
				if err := os.WriteFile(base, []byte("ordinary file"), 0600); err != nil {
					t.Fatal(err)
				}
			case "cover-link", "cover-file":
				if err := os.Mkdir(base, 0700); err != nil {
					t.Fatal(err)
				}
				cover := filepath.Join(base, "covers")
				if mode == "cover-link" {
					if err := os.Symlink(target, cover); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.WriteFile(cover, []byte("ordinary file"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			guard, err := PrepareCoverDirectory(context.Background(), base)
			if guard != nil {
				guard.Close()
				t.Fatal("partial guard returned")
			}
			if err == nil {
				t.Fatal("unsafe directory accepted")
			}
			if mode == "parent-link" || mode == "cover-link" || mode == "parent-file" {
				if !errors.Is(err, ErrUnsafeCoverDirectory) {
					t.Fatalf("wrong rejection: %v", err)
				}
			}
			if err := os.Rename(root, root+"-moved"); err != nil {
				t.Fatalf("leaked handles: %v", err)
			}
			got, err := os.ReadFile(sentinel)
			if err != nil || string(got) != "unchanged" {
				t.Fatal("target changed")
			}
			entries, err := os.ReadDir(target)
			if err != nil || len(entries) != 1 {
				t.Fatal("unexpected target mutation")
			}
		})
	}
}

func TestPrepareCoverDirectoryRejectsPathsAndCancellation(t *testing.T) {
	for _, name := range []string{"", `relative`, `C:relative`, `\rooted`, `\\server\share\app`, `\\?\C:\app`, `C:\`, `C:\a\..\app`, `C:\app\`, `C:/app`, `C:\con`, `C:\app:stream`, `C:\bad?name`, `C:\trailing.`, "C:\\nul\x00"} {
		if _, err := coverDirectoryPrefixes(name); !errors.Is(err, ErrUnsafeCoverDirectory) {
			t.Fatalf("accepted %q: %v", name, err)
		}
	}
	base := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	guard, err := PrepareCoverDirectory(ctx, base)
	if guard != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v %v", guard, err)
	}
	if _, err := os.Lstat(filepath.Join(base, "covers")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled operation created covers")
	}
}

func TestCoverDirectoryCloseAttemptsAllAndCachesFailure(t *testing.T) {
	failure := errors.New("synthetic close failure")
	var calls []windows.Handle
	g := &coverDirectoryGuard{handles: []windows.Handle{1, 2, 3}, closeHandle: func(h windows.Handle) error {
		calls = append(calls, h)
		if h == 2 {
			return failure
		}
		return nil
	}}
	if err := g.Close(); !errors.Is(err, failure) || !errors.Is(err, ErrCoverRelease) {
		t.Fatalf("close error lost: %v", err)
	}
	if err := g.Close(); !errors.Is(err, failure) || !errors.Is(err, ErrCoverRelease) {
		t.Fatalf("repeated close error lost: %v", err)
	}
	if !reflect.DeepEqual(calls, []windows.Handle{3, 2, 1}) {
		t.Fatalf("close order/count: %v", calls)
	}
}

func TestPrepareCoverDirectorySharingFailure(t *testing.T) {
	base := t.TempDir()
	p, err := windows.UTF16PtrFromString(base)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.DELETE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	guard, err := PrepareCoverDirectory(context.Background(), base)
	closeErr := windows.CloseHandle(h)
	if guard != nil {
		guard.Close()
		t.Fatal("partial guard returned")
	}
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) || closeErr != nil {
		t.Fatalf("sharing failure: %v %v", err, closeErr)
	}
	if _, err := os.Lstat(filepath.Join(base, "covers")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed operation created covers")
	}
	if err := os.Rename(base, base+"-moved"); err != nil {
		t.Fatalf("leaked ancestor: %v", err)
	}
}

type coverCancelContext struct {
	context.Context
	check func()
}

func (c coverCancelContext) Err() error {
	c.check()
	return c.Context.Err()
}

func TestPrepareCoverDirectoryCancellationAfterCreation(t *testing.T) {
	base := t.TempDir()
	cover := filepath.Join(base, "covers")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Trigger cancellation at the first context check after creation. This
	// controls timing without a race or a production filesystem hook.
	controlled := coverCancelContext{Context: ctx, check: func() {
		if _, err := os.Lstat(cover); err == nil {
			cancel()
		}
	}}
	guard, err := PrepareCoverDirectory(controlled, base)
	if guard != nil {
		guard.Close()
		t.Fatal("partial guard returned")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if info, err := os.Stat(cover); err != nil || !info.IsDir() {
		t.Fatalf("created directory removed: %v", err)
	}
	if err := os.Rename(base, base+"-moved"); err != nil {
		t.Fatalf("cancelled guard leaked: %v", err)
	}
}
