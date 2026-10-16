package coverfileboundaries

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// Test-only lexical subset, not an arbitrary Windows namespace resolver.
func probeDriveComponents(name string) ([]string, error) {
	volume := filepath.VolumeName(name)
	if len(volume) != 2 || volume[1] != ':' || !((volume[0] >= 'A' && volume[0] <= 'Z') || (volume[0] >= 'a' && volume[0] <= 'z')) || !filepath.IsAbs(name) || strings.ContainsAny(name, "/\x00") || filepath.Clean(name) != name {
		return nil, errors.New("unsupported synthetic path")
	}
	paths := []string{volume + `\`}
	if name == paths[0] {
		return paths, nil
	}
	for _, component := range strings.Split(name[3:], `\`) {
		if component == "" || strings.Contains(component, ":") || strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") {
			return nil, errors.New("unsupported synthetic component")
		}
		paths = append(paths, filepath.Join(paths[len(paths)-1], component))
	}
	return paths, nil
}

func probeLocalDrive(kind uint32) bool {
	return kind == windows.DRIVE_FIXED || kind == windows.DRIVE_REMOVABLE || kind == windows.DRIVE_RAMDISK
}

// Each parent stays held while the next absolute component is opened.
// Drive-letter remapping and concurrent reparse-attribute changes are not solved.
func holdDriveProbe(name string) ([]windows.Handle, error) {
	return holdDriveProbeWithAccess(name, windows.GENERIC_READ)
}

func holdDriveProbeWithAccess(name string, access uint32) ([]windows.Handle, error) {
	paths, err := probeDriveComponents(name)
	if err != nil {
		return nil, err
	}
	root, err := windows.UTF16PtrFromString(paths[0])
	if err != nil {
		return nil, err
	}
	if !probeLocalDrive(windows.GetDriveType(root)) {
		return nil, errors.New("unsupported synthetic drive type")
	}
	var held []windows.Handle
	for _, path := range paths {
		p, err := windows.UTF16PtrFromString(path)
		if err != nil {
			return nil, errors.Join(err, closeProbeChain(held))
		}
		h, err := windows.CreateFile(p, access,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING,
			windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
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
			return nil, errors.Join(errUnsafeProbeDirectory, closeProbeChain(held))
		}
	}
	return held, nil
}

func TestDriveProbePathSubset(t *testing.T) {
	for _, name := range []string{`relative`, `C:relative`, `\rooted`, `\\server\share\covers`, `\\?\C:\covers`, `C:\a\..\b`, `C:\a\`, `C:\name:stream`, "C:\\nul\x00", `C:\trailing.`, `C:/covers`} {
		if paths, err := probeDriveComponents(name); err == nil || paths != nil {
			t.Fatalf("unsupported path accepted: %q %v", name, err)
		}
	}
	paths, err := probeDriveComponents(`C:\synthetic\covers`)
	if err != nil || strings.Join(paths, "|") != `C:\|C:\synthetic|C:\synthetic\covers` {
		t.Fatalf("components: %v %v", paths, err)
	}
	for _, kind := range []uint32{windows.DRIVE_UNKNOWN, windows.DRIVE_NO_ROOT_DIR, windows.DRIVE_REMOTE, windows.DRIVE_CDROM} {
		if probeLocalDrive(kind) {
			t.Fatalf("unsupported drive accepted: %d", kind)
		}
	}
}

func TestDriveMetadataOnlyObservesRename(t *testing.T) {
	root := syntheticTree(t)
	held, err := holdDriveProbeWithAccess(filepath.Join(root, "managed", "covers"), windows.FILE_READ_ATTRIBUTES)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := closeProbeChain(held); err != nil {
			t.Error(err)
		}
	}()
	err = os.Rename(root, root+"-moved")
	if err == nil {
		t.Log("metadata-only handles allowed ancestor rename; not a safe guard")
	} else if errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Logf("metadata-only rename rejected in this run: %v; not a protection guarantee", err)
	} else {
		t.Fatal(err)
	}
}

func TestDriveRootSharedHoldAndRelease(t *testing.T) {
	root := syntheticTree(t)
	leaf := filepath.Join(root, "managed", "covers")
	held, err := holdDriveProbe(leaf)
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
	// The second acquisition must coexist, unlike the earlier share-zero probe.
	other, err := holdDriveProbe(leaf)
	if err != nil {
		t.Fatalf("compatible metadata reader blocked: %v", err)
	}
	if err := closeProbeChain(other); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadDir(leaf); err != nil {
		t.Fatalf("directory listing blocked: %v", err)
	}
	name := filepath.Join(leaf, "synthetic.txt")
	if err := os.WriteFile(name, []byte("shared hold payload"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(name)
	if err != nil || string(got) != "shared hold payload" {
		t.Fatalf("child read/write blocked: %q %v", got, err)
	}
	for _, dir := range []string{root, filepath.Join(root, "managed"), leaf} {
		err := os.Rename(dir, dir+"-moved")
		if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			t.Fatalf("rename not blocked: %v", err)
		}
	}
	if err := closeProbeChain(held); err != nil {
		held = nil
		t.Fatal(err)
	}
	held = nil
	for _, dir := range []string{leaf, filepath.Join(root, "managed"), root} {
		if err := os.Rename(dir, dir+"-moved"); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(dir+"-moved", dir); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDriveRootFailuresReleaseAncestors(t *testing.T) {
	for _, mode := range []string{"symlink", "file", "missing"} {
		t.Run(mode, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "root")
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			parent := filepath.Join(root, "managed")
			target := t.TempDir()
			sentinel := filepath.Join(target, "sentinel.txt")
			if err := os.WriteFile(sentinel, []byte("unchanged"), 0600); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "symlink":
				if err := os.Symlink(target, parent); err != nil {
					t.Fatal(err)
				}
			case "file":
				if err := os.WriteFile(parent, []byte("not a directory"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			held, err := holdDriveProbe(filepath.Join(parent, "covers"))
			if held != nil {
				closeProbeChain(held)
				t.Fatal("partial handles returned")
			}
			if err == nil || (mode != "missing" && !errors.Is(err, errUnsafeProbeDirectory)) {
				t.Fatalf("wrong rejection for %s: %v", mode, err)
			}
			if err := os.Rename(root, root+"-moved"); err != nil {
				t.Fatalf("ancestor leaked: %v", err)
			}
			got, err := os.ReadFile(sentinel)
			if err != nil || string(got) != "unchanged" {
				t.Fatal("target changed")
			}
			entries, err := os.ReadDir(target)
			if err != nil || len(entries) != 1 {
				t.Fatal("unexpected target contents")
			}
		})
	}
}

func TestDriveProbeExistingDeleteHandleFailsClosed(t *testing.T) {
	root := syntheticTree(t)
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.DELETE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	held, err := holdDriveProbe(filepath.Join(root, "managed", "covers"))
	if held != nil {
		closeProbeChain(held)
		t.Fatal("partial handles returned")
	}
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatal(fmt.Errorf("expected sharing conflict: %w", err))
	}
}
