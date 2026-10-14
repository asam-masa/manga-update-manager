package coverfileboundaries

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

var errUnsafeProbeDirectory = errors.New("unsafe synthetic directory")

// holdProbeChain is limited to test-owned root/managed/covers, not a production
// trust anchor or arbitrary path resolver. Retain each handle before opening
// the next component; OPEN_REPARSE_POINT only applies to the final component.
func holdProbeChain(root string) ([]windows.Handle, error) {
	var held []windows.Handle
	name := root
	for _, component := range []string{"", "managed", "covers"} {
		if component != "" {
			name = filepath.Join(name, component)
		}
		h, err := openProbe(name, windows.GENERIC_READ, windows.OPEN_EXISTING)
		if err != nil {
			return nil, errors.Join(err, closeProbeChain(held))
		}
		held = append(held, h)
		var info windows.ByHandleFileInformation
		if err := windows.GetFileInformationByHandle(h, &info); err != nil {
			return nil, errors.Join(err, closeProbeChain(held))
		}
		kind, err := windows.GetFileType(h)
		if err != nil {
			return nil, errors.Join(err, closeProbeChain(held))
		}
		if kind != windows.FILE_TYPE_DISK || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return nil, errors.Join(fmt.Errorf("%w: %s", errUnsafeProbeDirectory, component), closeProbeChain(held))
		}
	}
	return held, nil
}

func closeProbeChain(held []windows.Handle) error {
	var result error
	for i := len(held) - 1; i >= 0; i-- {
		result = errors.Join(result, windows.CloseHandle(held[i]))
	}
	return result
}

func syntheticTree(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "root")
	if err := os.MkdirAll(filepath.Join(root, "managed", "covers"), 0700); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestChildHandleAloneObservesParentRename(t *testing.T) {
	root := syntheticTree(t)
	parent := filepath.Join(root, "managed")
	h, err := openProbe(filepath.Join(parent, "covers"), windows.GENERIC_READ, windows.OPEN_EXISTING)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	err = os.Rename(parent, filepath.Join(root, "moved"))
	if err == nil {
		t.Log("parent rename succeeded while only covers was held: child handle is insufficient in this environment")
	} else if errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Logf("parent rename rejected in this environment: %v; do not generalize child-only protection", err)
	} else {
		t.Fatalf("unexpected parent rename error: %v", err)
	}
}

func TestHeldAncestorChainBlocksRenameAndAllowsCreate(t *testing.T) {
	root := syntheticTree(t)
	held, err := holdProbeChain(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if held != nil {
			if err := closeProbeChain(held); err != nil {
				t.Error(err)
			}
		}
	}()
	for _, name := range []string{root, filepath.Join(root, "managed"), filepath.Join(root, "managed", "covers")} {
		err := os.Rename(name, name+"-moved")
		if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			t.Fatalf("held ancestor rename not rejected: %v", err)
		}
	}
	name := filepath.Join(root, "managed", "covers", "synthetic.png")
	h, err := openProbe(name, windows.GENERIC_WRITE, windows.CREATE_NEW)
	if err != nil {
		t.Fatal(err)
	}
	file := os.NewFile(uintptr(h), name)
	defer file.Close()
	if _, err := file.Write([]byte("synthetic chain payload")); err != nil {
		t.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := closeProbeChain(held); err != nil {
		held = nil
		t.Fatal(err)
	}
	held = nil
	for _, name := range []string{filepath.Join(root, "managed", "covers"), filepath.Join(root, "managed"), root} {
		if err := os.Rename(name, name+"-moved"); err != nil {
			t.Fatalf("rename after release: %v", err)
		}
		if err := os.Rename(name+"-moved", name); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(name)
	if err != nil || string(got) != "synthetic chain payload" {
		t.Fatalf("payload changed: %q %v", got, err)
	}
}

func TestAncestorChainRejectsDirectorySymlink(t *testing.T) {
	root := syntheticTree(t)
	parent := filepath.Join(root, "managed")
	if err := os.Rename(parent, filepath.Join(root, "original-managed")); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.MkdirAll(filepath.Join(target, "covers"), 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(target, "covers", "sentinel.png")
	if err := os.WriteFile(sentinel, []byte("preserved synthetic target"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, parent); err != nil {
		t.Fatal(err)
	}
	held, err := holdProbeChain(root)
	if !errors.Is(err, errUnsafeProbeDirectory) {
		closeProbeChain(held)
		t.Fatalf("expected attribute rejection, got: %v", err)
	}
	if held != nil {
		t.Fatal("partial handles returned on failure")
	}
	got, err := os.ReadFile(sentinel)
	if err != nil || string(got) != "preserved synthetic target" {
		t.Fatal("symlink target changed")
	}
	entries, err := os.ReadDir(filepath.Join(target, "covers"))
	if err != nil || len(entries) != 1 {
		t.Fatal("unexpected target mutation")
	}
	// Failed acquisition must release the already-held root.
	if err := os.Rename(root, root+"-moved"); err != nil {
		t.Fatalf("root handle leaked after rejection: %v", err)
	}
}
